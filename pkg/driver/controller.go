package driver

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/vpsie/govpsie"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
)

const (
	_   = iota
	kiB = 1 << (10 * iota)
	miB
	giB
	tiB
)

const (
	// minimumVolumeSizeInBytes is used to validate that the user is not trying
	// to create a volume that is smaller than what we support
	minimumVolumeSizeInBytes int64 = 1 * giB

	// maximumVolumeSizeInBytes is used to validate that the user is not trying
	// to create a volume that is larger than what we support
	maximumVolumeSizeInBytes int64 = 16 * tiB

	// defaultVolumeSizeInBytes is used when the user did not provide a size or
	// the size they provided did not satisfy our requirements
	defaultVolumeSizeInBytes int64 = 16 * giB

	createdByVpsie = "Created by Vpsie CSI driver"

	defaultVolumesPageSize = 10
)

var (
	supportedAccessMode = &csi.VolumeCapability_AccessMode{
		Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
	}
)

type NotFoundError struct {
	msg string
}

// Error implements the error interface for NotFoundError.
func (e *NotFoundError) Error() string {
	return e.msg
}

// IsNotFoundError checks if the given error is a NotFoundError.
func IsNotFoundError(err error) bool {
	_, ok := err.(*NotFoundError)
	return ok
}

type VolumeMap struct {
	Name          string
	LastAccessed  time.Time
	Failures      int
	TotalFailures int
}

var VolumeFailures = make(map[string]*VolumeMap)
var VolumeFailuresMu sync.Mutex
var CleanupThreshold = time.Hour * 1

// Function to increment the failure count for a volume name
func incrementFailureCount(volumeName string) {
	VolumeFailuresMu.Lock()
	defer VolumeFailuresMu.Unlock()

	volume, exists := VolumeFailures[volumeName]
	if exists {
		volume.LastAccessed = time.Now()
		volume.Failures++
		volume.TotalFailures++
	} else {
		VolumeFailures[volumeName] = &VolumeMap{
			Name:          volumeName,
			LastAccessed:  time.Now(),
			Failures:      1,
			TotalFailures: 1,
		}
	}
}

// Function to check if the maximum retry count is exceeded for a volume name
func isRetryLimitExceeded(volumeName string, maxRetries int) bool {
	VolumeFailuresMu.Lock()
	defer VolumeFailuresMu.Unlock()

	volume, exists := VolumeFailures[volumeName]
	if !exists {
		return false
	}

	return volume.Failures >= maxRetries
}

// Function to reset the failure count for a volume name
func resetFailureCount(volumeName string) {
	VolumeFailuresMu.Lock()
	defer VolumeFailuresMu.Unlock()

	volume, exists := VolumeFailures[volumeName]
	if exists {
		volume.Failures = 0
	}
}

func getTotalFailures(volumeName string) int {
	VolumeFailuresMu.Lock()
	defer VolumeFailuresMu.Unlock()

	volume, exists := VolumeFailures[volumeName]
	if !exists {
		return 0
	}

	return volume.TotalFailures
}

func (d *Driver) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "CreateVolume Name must be provided")
	}

	if req.VolumeCapabilities == nil || len(req.VolumeCapabilities) == 0 {
		return nil, status.Error(codes.InvalidArgument, "CreateVolume Volume capabilities must be provided")
	}

	if violations := validateCapabilities(req.VolumeCapabilities); len(violations) > 0 {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("volume capabilities cannot be satisified: %s", strings.Join(violations, "; ")))
	}

	size, err := d.extractStorage(req.CapacityRange)
	if err != nil {
		return nil, status.Errorf(codes.OutOfRange, "invalid capacity range: %v", err)
	}

	var (
		storageType string
	)

	for key, value := range req.GetParameters() {
		switch strings.ToLower(key) {
		case "type":
			storageType = strings.ToUpper(value)
			klog.Infof("storageType: %s", storageType)
		default:
			return nil, status.Errorf(codes.InvalidArgument, "Invalid parameter key %s for CreateVolume", key)
		}
	}

	volumeName := req.Name
	klog.Infof("Create Volume Param: type: %s", storageType)
	klog.Infof("volume_name: %s, size: %d, method: %s, volume_capabilities: %s", volumeName, size, "create_volume", req.GetVolumeCapabilities)

	klog.Info("create volume called")

	volume, err := d.getVolumeByName(ctx, volumeName)

	if err == nil {
		if int64(volume.Size)*giB != size {
			return nil, status.Error(codes.AlreadyExists, fmt.Sprintf("invalid option requested size: %d", size))
		}

		klog.Info("volume already created")
		return &csi.CreateVolumeResponse{
			Volume: &csi.Volume{
				VolumeId:      volume.Identifier,
				CapacityBytes: int64(volume.Size) * giB,
			},
		}, nil
	}

	if err != nil && !IsNotFoundError(err) {
		return nil, status.Error(codes.Internal, err.Error())
	}

	createStorageRequest := &govpsie.StorageCreateRequest{
		Name:         volumeName,
		DcIdentifier: d.config.DataCenter,
		DiskFormat:   "EXT4",
		Size:         int(size / giB),
		Description:  createdByVpsie,
		StorageType:  storageType,
		IsAutomatic:  0,
		K8sTrials:    getTotalFailures(volumeName),
	}
	// if d.config.StorageTag != "" {
	// 	createStorageRequest.Tags = append(createStorageRequest.Tags, d.config.StorageTag)
	// }

	klog.Infof("volume_req: %s\n , creating volume", createStorageRequest)

	contentSource := req.GetVolumeContentSource()
	if contentSource != nil && contentSource.GetSnapshot() != nil {
		snapshotID := contentSource.GetSnapshot().GetSnapshotId()
		if snapshotID == "" {
			return nil, status.Error(codes.InvalidArgument, "snapshot ID is empty")
		}

		_, err := d.getSnapshotByIdentifier(ctx, snapshotID)
		if err != nil {
			if err.Error() == "NotFound" {
				return nil, status.Errorf(codes.NotFound, "snapshot %q not found", snapshotID)
			}

			return nil, err
		}

		err = d.storage.CloneSnapshot(ctx, snapshotID, "boxes")
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

	} else {
		// Check if maximum retry count is exceeded
		if isRetryLimitExceeded(volumeName, d.config.MaxRetries) {
			klog.Errorf("Retry limit exceeded for volume: %s\n", volumeName)
			return nil, status.Error(codes.ResourceExhausted, fmt.Sprintf("retry limit exceeded for volume: %s", volumeName))
		}

		err = d.storage.CreateVolume(ctx, createStorageRequest)
		if err != nil {
			s, _ := json.MarshalIndent(createStorageRequest, "", "\t")
			klog.Error("json request to vpsie: %s", string(s))
			klog.Error("Error from vpsie: %v", err)

			klog.Errorf("Error creating volume: %v", err)

			incrementFailureCount(volumeName)

			if err.Error() == "At the moment, Storage is not available in this Datacenter, please try a different option " {
				return nil, status.Error(codes.ResourceExhausted, err.Error())
			}

			return nil, status.Error(codes.Internal, err.Error())
		}

		// Reset failure count on successful creation
		resetFailureCount(volumeName)
	}

	vol, err := d.getVolumeByName(ctx, volumeName)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if vol == nil {
		return nil, status.Error(codes.NotFound, "volume not Created")
	}

	if vol.Size < int(size/giB) {
		err := d.storage.UpdateSize(ctx, vol.Identifier, string(vol.Size))
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	resp := &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      vol.Identifier,
			CapacityBytes: size,
			AccessibleTopology: []*csi.Topology{
				{
					Segments: map[string]string{
						"region": d.config.DataCenter,
					},
				},
			},
		},
	}

	klog.Infof("response: %s, volume was created", resp)
	return resp, nil

}

func (d *Driver) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "DeleteVolume Volume ID must be provided")
	}

	err := d.storage.Delete(ctx, req.VolumeId)
	if err != nil {
		return &csi.DeleteVolumeResponse{}, nil
	}

	klog.Infof("volume %v is deleted", req.VolumeId)

	return &csi.DeleteVolumeResponse{}, nil
}

func (d *Driver) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "ControllerPublishVolume Volume ID must be provided")
	}

	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "ControllerPublishVolume Node ID must be provided")
	}

	if req.VolumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "ControllerPublishVolume Volume capability must be provided")
	}

	if req.Readonly {
		return nil, status.Error(codes.AlreadyExists, "read only Volumes are not supported")
	}

	klog.Infof("controller publish volume called, volume_id: %v, node_id: %v", req.VolumeId, req.NodeId)

	storage, err := d.getStorage(ctx, req.VolumeId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "ControllerPublishVolume Volume do not exist")
	}

	if storage.VmIdentifier != "" {
		if storage.VmIdentifier == req.NodeId {
			return &csi.ControllerPublishVolumeResponse{
				PublishContext: map[string]string{
					d.publishInfoVolumeName: req.VolumeId,
				},
			}, nil
		} else {
			return nil, status.Error(codes.AlreadyExists, "ControllerPublishVolume Volume is already attached to another node")
		}
	}

	// _, err = d.vpsie.GetVpsieByIdentifier(ctx, req.NodeId)
	// if err != nil {
	// 	return nil, status.Error(codes.NotFound, "ControllerPublishVolume Node do not exist")
	// }

	vms, err := d.storage.ListVmsToAttach(context.Background())
	if err != nil {
		return nil, err
	}

	if vms == nil || len(vms) < 1 {
		return nil, status.Error(codes.NotFound, "No VM to attach")
	}

	var vm *govpsie.VmToAttach

	for _, vmToAttach := range vms {
		if vmToAttach.Identifier == req.NodeId {
			vm = &vmToAttach
		}
	}

	// attach the volume to the correct node
	err = d.storage.AttachToServer(ctx, req.VolumeId, req.NodeId, vm.Type)
	if err != nil {
		klog.Errorf("failed to attach volume: %v", err)
		return nil, status.Error(codes.Internal, "ControllerPublishVolume failed to attach volume")
	}

	klog.Info("volume was attached")
	return &csi.ControllerPublishVolumeResponse{
		PublishContext: map[string]string{
			d.publishInfoVolumeName: req.VolumeId,
		},
	}, nil
}

func (d *Driver) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	klog.V(4).InfoS("ControllerUnpublishVolume: called", "args", *req)
	volumeID := req.GetVolumeId()
	nodeID := req.GetNodeId()

	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	if len(nodeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Node ID not provided")
	}

	_, err := d.getStorage(ctx, req.VolumeId)
	if err != nil {
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	// _, err = d.vpsie.GetVpsieByIdentifier(ctx, req.NodeId)
	// if err != nil {
	// 	return &csi.ControllerUnpublishVolumeResponse{}, nil
	// }

	vms, err := d.storage.ListVmsToAttach(context.Background())
	if err != nil {
		return nil, err
	}

	if vms == nil || len(vms) < 1 {
		return nil, status.Error(codes.NotFound, "No VM to attach")
	}

	var vm *govpsie.VmToAttach

	for _, vmToAttach := range vms {
		if vmToAttach.Identifier == nodeID {
			vm = &vmToAttach
		}
	}

	if err := d.storage.DetachToServer(ctx, volumeID, nodeID, vm.Type); err != nil {
		return nil, err
	}

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (d *Driver) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	klog.V(4).InfoS("ValidateVolumeCapabilities: called", "args", *req)
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	volCaps := req.GetVolumeCapabilities()
	if len(volCaps) == 0 || volCaps == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capabilities not provided")
	}

	_, err := d.getStorage(ctx, volumeID)
	if err != nil {
		return nil, status.Error(codes.NotFound, "Volume Not found")
	}

	klog.Infof("validate volume capabilities called, volume_id: %v, volume_capabilities: %v", req.VolumeId, req.VolumeCapabilities)

	resp := &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessMode: supportedAccessMode,
				},
			},
		},
	}

	return resp, nil
}

// ListVolumes returns a list of all requested volumes
func (d *Driver) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	maxEntries := req.MaxEntries
	if maxEntries == 0 && defaultVolumesPageSize > 0 {
		maxEntries = int32(defaultVolumesPageSize)
	}

	var startingToken int32
	if req.StartingToken != "" {
		parsedToken, err := strconv.ParseInt(req.StartingToken, 10, 32)
		if err != nil {
			return nil, status.Errorf(codes.Aborted, "ListVolumes starting token %q is not valid: %s", req.StartingToken, err)
		}
		startingToken = int32(parsedToken)
	}

	options := govpsie.ListOptions{
		Page:    int(startingToken/maxEntries) + 1,
		PerPage: int(maxEntries),
	}
	volumes, err := d.storage.List(ctx, &options)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "ListVolumes failed: %s", err)
	}

	var nextToken int
	if len(volumes) == int(maxEntries) {
		nextToken = int(startingToken) + len(volumes)
	} else {
		nextToken = 0
	}

	var entries []*csi.ListVolumesResponse_Entry
	for _, vol := range volumes {

		entries = append(entries, &csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				VolumeId:      vol.Identifier,
				CapacityBytes: int64(vol.Size) * giB,
			},
			Status: &csi.ListVolumesResponse_VolumeStatus{
				PublishedNodeIds: []string{vol.VmIdentifier},
			},
		})
	}

	resp := &csi.ListVolumesResponse{
		Entries: entries,
	}

	if nextToken > 0 {
		resp.NextToken = strconv.FormatInt(int64(nextToken), 10)
	}

	return resp, nil
}

// GetCapacity returns the capacity of the storage pool
func (d *Driver) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
func (d *Driver) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	newCap := func(cap csi.ControllerServiceCapability_RPC_Type) *csi.ControllerServiceCapability {
		return &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: cap,
				},
			},
		}
	}

	var caps []*csi.ControllerServiceCapability
	for _, cap := range []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
		// csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
		csi.ControllerServiceCapability_RPC_LIST_VOLUMES_PUBLISHED_NODES,
	} {
		caps = append(caps, newCap(cap))
	}

	resp := &csi.ControllerGetCapabilitiesResponse{
		Capabilities: caps,
	}

	return resp, nil
}

func (d *Driver) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	name := req.GetName()
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "CreateSnapshot name must be provided")
	}

	source_volume_id := req.GetSourceVolumeId()
	if source_volume_id == "" {
		return nil, status.Error(codes.InvalidArgument, "CreateSnapshot source volume ID must be provided")
	}

	klog.Info("create snapshot called")

	snapshots, err := d.storage.ListSnapshots(ctx, &govpsie.ListOptions{Page: 0, PerPage: 10000})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	volumeMap, err := d.volumeMap(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	for _, snapshot := range snapshots {
		storage_identifier, ok := volumeMap[snapshot.StorageID]
		if !ok {
			return nil, status.Error(codes.Internal, "volume not found")
		}
		if snapshot.Name == name && storage_identifier == source_volume_id {
			return &csi.CreateSnapshotResponse{
				Snapshot: convertToCSISnapshot(&snapshot, source_volume_id),
			}, nil
		}
	}

	storageType := req.GetParameters()["type"]
	err = d.storage.CreateSnapshot(ctx, source_volume_id, name, storageType)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	klog.Infof("snapshot %v is created", name)
	snapshot, err := d.getSnapshotByName(ctx, name)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.CreateSnapshotResponse{
		Snapshot: convertToCSISnapshot(snapshot, source_volume_id),
	}, nil
}

func (d *Driver) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	klog.Infof("delete snapshot called:  %s", req.SnapshotId)
	snapshotID := req.GetSnapshotId()
	if snapshotID == "" {
		return nil, status.Error(codes.InvalidArgument, "DeleteSnapshot Snapshot ID must be provided")
	}

	err := d.storage.DeleteSnapshot(ctx, snapshotID)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	klog.Infof("snapshot %v is deleted", req.SnapshotId)
	return &csi.DeleteSnapshotResponse{}, nil
}

func (d *Driver) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	klog.Infof("list snapshot called for %v", req.GetSnapshotId())

	snapListRsp := &csi.ListSnapshotsResponse{}

	snapshotID := req.GetSnapshotId()
	if snapshotID == "" {
		var startingToken int32
		if req.StartingToken != "" {
			parsedToken, err := strconv.ParseInt(req.StartingToken, 10, 32)
			if err != nil {
				return nil, status.Errorf(codes.Aborted, "ListSnapshots starting token %q is not valid: %s", req.StartingToken, err)
			}
			startingToken = int32(parsedToken)
		}

		options := govpsie.ListOptions{
			Page:    int(startingToken),
			PerPage: int(req.MaxEntries),
		}

		snapshots, err := d.storage.ListSnapshots(ctx, &options)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		entries := make([]*csi.ListSnapshotsResponse_Entry, 0, len(snapshots))
		volumeMaps, err := d.volumeMap(ctx)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		for _, snapshot := range snapshots {
			storage_identifier, ok := volumeMaps[snapshot.StorageID]
			if !ok {
				return nil, status.Errorf(codes.Internal, "source volume %q not found", snapshot.StorageID)
			}
			entries = append(entries, &csi.ListSnapshotsResponse_Entry{
				Snapshot: convertToCSISnapshot(&snapshot, storage_identifier),
			})
		}

		snapListRsp = &csi.ListSnapshotsResponse{
			Entries: entries,
		}

		if len(snapshots) == int(req.MaxEntries) {
			snapListRsp.NextToken = strconv.Itoa(int(startingToken + 1))
		}

	} else {
		snapshot, err := d.getSnapshotByIdentifier(ctx, snapshotID)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		volumeMaps, err := d.volumeMap(ctx)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		storage_identifier, ok := volumeMaps[snapshot.StorageID]
		if !ok {
			return nil, status.Errorf(codes.Internal, "source volume %q not found", snapshot.StorageID)
		}

		snapListRsp = &csi.ListSnapshotsResponse{
			Entries: []*csi.ListSnapshotsResponse_Entry{
				{
					Snapshot: convertToCSISnapshot(snapshot, storage_identifier),
				},
			},
		}
	}

	return snapListRsp, nil
}

func (d *Driver) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	volID := req.GetVolumeId()

	if volID == "" {
		return nil, status.Error(codes.InvalidArgument, "ControllerExpandVolume volume ID missing in request")
	}

	resizeBytes, err := d.extractStorage(req.GetCapacityRange())
	if err != nil {
		return nil, status.Errorf(codes.OutOfRange, "ControllerExpandVolume invalid capacity range: %v", err)
	}
	resizeGigaBytes := resizeBytes / giB

	klog.Info("controller expand volume called")

	if err := d.storage.UpdateSize(ctx, volID, string(resizeGigaBytes)); err != nil {
		return nil, status.Errorf(codes.Internal, "failed updating storage")
	}

	klog.Info("volume was resized")

	nodeExpansionRequired := true
	if req.GetVolumeCapability() != nil {
		if _, ok := req.GetVolumeCapability().GetAccessType().(*csi.VolumeCapability_Block); ok {
			klog.Info("node expansion is not required for block volumes")
			nodeExpansionRequired = false
		}
	}

	return &csi.ControllerExpandVolumeResponse{CapacityBytes: resizeBytes, NodeExpansionRequired: nodeExpansionRequired}, nil
}

func (d *Driver) ControllerGetVolume(context.Context, *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ControllerGetVolume not implemented")
}

func (d *Driver) extractStorage(capRange *csi.CapacityRange) (int64, error) {
	if capRange == nil {
		return defaultVolumeSizeInBytes, nil
	}

	requiredBytes := capRange.GetRequiredBytes()
	requiredSet := 0 < requiredBytes
	limitBytes := capRange.GetLimitBytes()
	limitSet := 0 < limitBytes

	if !requiredSet && !limitSet {
		return defaultVolumeSizeInBytes, nil
	}

	if requiredSet && limitSet && limitBytes < requiredBytes {
		return 0, fmt.Errorf("limit (%v) can not be less than required (%v) size", formatBytes(limitBytes), formatBytes(requiredBytes))
	}

	if requiredSet && !limitSet && requiredBytes < minimumVolumeSizeInBytes {
		klog.Warningf("required_bytes (%v) is less than minimum_volume_size_in_bytes (%v)", formatBytes(requiredBytes), formatBytes(minimumVolumeSizeInBytes))

		return minimumVolumeSizeInBytes, nil
	}

	if limitSet && limitBytes < minimumVolumeSizeInBytes {
		return 0, fmt.Errorf("limit (%v) can not be less than minimum supported volume size (%v)", formatBytes(limitBytes), formatBytes(minimumVolumeSizeInBytes))
	}

	if requiredSet && requiredBytes > maximumVolumeSizeInBytes {
		return 0, fmt.Errorf("required (%v) can not exceed maximum supported volume size (%v)", formatBytes(requiredBytes), formatBytes(maximumVolumeSizeInBytes))
	}

	if !requiredSet && limitSet && limitBytes > maximumVolumeSizeInBytes {
		return 0, fmt.Errorf("limit (%v) can not exceed maximum supported volume size (%v)", formatBytes(limitBytes), formatBytes(maximumVolumeSizeInBytes))
	}

	if requiredSet && limitSet && requiredBytes == limitBytes {
		return requiredBytes, nil
	}

	if requiredSet {
		return requiredBytes, nil
	}

	if limitSet {
		return limitBytes, nil
	}

	return defaultVolumeSizeInBytes, nil
}

func formatBytes(inputBytes int64) string {
	output := float64(inputBytes)
	unit := ""

	switch {
	case inputBytes >= tiB:
		output = output / tiB
		unit = "Ti"
	case inputBytes >= giB:
		output = output / giB
		unit = "Gi"
	case inputBytes >= miB:
		output = output / miB
		unit = "Mi"
	case inputBytes >= kiB:
		output = output / kiB
		unit = "Ki"
	case inputBytes == 0:
		return "0"
	}

	result := strconv.FormatFloat(output, 'f', 1, 64)
	result = strings.TrimSuffix(result, ".0")
	return result + unit
}

func isValidVolumeContext(volContext map[string]string) bool {
	//There could be multiple volume attributes in the volumeContext map
	//Validate here case by case
	if partition, ok := volContext["partition"]; ok {
		partitionInt, err := strconv.ParseInt(partition, 10, 64)
		if err != nil {
			klog.ErrorS(err, "failed to parse partition as int", "partition", partition)
			return false
		}
		if partitionInt < 0 {
			klog.ErrorS(err, "invalid partition config", "partition", partition)
			return false
		}
	}
	return true
}

func (d *Driver) getStorage(ctx context.Context, storageIdentifier string) (*govpsie.Storage, error) {
	volumes, err := d.storage.List(ctx, &govpsie.ListOptions{
		Page:    0,
		PerPage: 1000,
	})
	if err != nil {
		klog.Errorf("failed to list volumes: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}

	for _, volume := range volumes {
		if volume.Identifier == storageIdentifier {
			return &volume, nil
		}
	}

	klog.Errorf("volume %s not found", storageIdentifier)
	return nil, fmt.Errorf("volume %s not found", storageIdentifier)
}

func validateCapabilities(caps []*csi.VolumeCapability) []string {
	violations := sets.NewString()
	for _, cap := range caps {
		if cap.GetAccessMode().GetMode() != supportedAccessMode.GetMode() {
			violations.Insert(fmt.Sprintf("unsupported access mode %s", cap.GetAccessMode().GetMode().String()))
		}

		accessType := cap.GetAccessType()
		switch accessType.(type) {
		case *csi.VolumeCapability_Block:
		case *csi.VolumeCapability_Mount:
		default:
			violations.Insert("unsupported access type")
		}
	}

	return violations.List()
}

func contains(slice []string, target string) bool {
	for _, value := range slice {
		if value == target {
			return true
		}
	}
	return false
}

func (d *Driver) getSnapshotByName(ctx context.Context, snapshotName string) (*govpsie.StorageSnapShot, error) {
	page := 0
	for {
		snapshots, err := d.storage.ListSnapshots(ctx, &govpsie.ListOptions{Page: page, PerPage: 500})

		if err != nil {
			klog.Errorf("failed to list snapshots: %v", err)
			return nil, status.Error(codes.Internal, err.Error())
		}

		for _, snapshot := range snapshots {
			if snapshot.Identifier == snapshotName {
				return &snapshot, nil
			}
		}

		if len(snapshots) < 500 {
			break
		}

		page += 1
	}

	klog.Errorf("snapshot %s not found", snapshotName)
	return nil, &NotFoundError{msg: fmt.Sprintf("snapshot %s not found", snapshotName)}
}

func (d *Driver) getSnapshotByIdentifier(ctx context.Context, snapshotIdentifier string) (*govpsie.StorageSnapShot, error) {
	page := 0
	for {
		snapshots, err := d.storage.ListSnapshots(ctx, &govpsie.ListOptions{Page: page, PerPage: 500})

		if err != nil {
			klog.Errorf("failed to list snapshots: %v", err)
			return nil, status.Error(codes.Internal, err.Error())
		}

		for _, snapshot := range snapshots {
			if snapshot.Identifier == snapshotIdentifier {
				return &snapshot, nil
			}
		}

		if len(snapshots) < 500 {
			break
		}

		page += 1
	}

	klog.Errorf("snapshot %s not found", snapshotIdentifier)
	return nil, &NotFoundError{msg: fmt.Sprintf("snapshot %s not found", snapshotIdentifier)}
}

func (d *Driver) getVolumeByName(ctx context.Context, volumeName string) (*govpsie.Storage, error) {
	page := 0
	for {
		volumes, err := d.storage.List(ctx, &govpsie.ListOptions{Page: page, PerPage: 500})
		if err != nil {
			klog.Errorf("failed to list snapshots: %v", err)
			return nil, status.Error(codes.Internal, err.Error())
		}

		for _, volume := range volumes {
			if volume.Name == volumeName {
				return &volume, nil
			}
		}

		if len(volumes) < 500 {
			break
		}

		page += 1
	}

	return nil, &NotFoundError{msg: fmt.Sprintf("volume %s not found", volumeName)}
}

func convertToCSISnapshot(snapshot *govpsie.StorageSnapShot, sourceVolumeIdentifier string) *csi.Snapshot {
	return &csi.Snapshot{
		SnapshotId:     snapshot.Identifier,
		SourceVolumeId: sourceVolumeIdentifier,
		CreationTime:   timestamppb.New(snapshot.CreatedOn),
		ReadyToUse:     true,
		SizeBytes:      int64(snapshot.Size) * giB,
	}
}

func (d *Driver) volumeMap(ctx context.Context) (map[int]string, error) {
	volumeMap := make(map[int]string)

	page := 0
	for {
		volumes, err := d.storage.List(ctx, &govpsie.ListOptions{Page: page, PerPage: 500})
		if err != nil {
			klog.Errorf("failed to list snapshots: %v", err)
			return nil, status.Error(codes.Internal, err.Error())
		}

		for _, volume := range volumes {
			volumeMap[volume.ID] = volume.Identifier
		}

		if len(volumes) < 500 {
			break
		}

		page += 1
	}

	return volumeMap, nil
}
