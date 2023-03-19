package driver

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"code.k9.ms/vpsie-csi/pkg/govpsie"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

	createdByDO = "Created by Vpsie CSI driver"
)

func (d *Driver) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "CreateVolume Name must be provided")
	}

	size, err := d.extractStorage(req.CapacityRange)
	if err != nil {
		return nil, status.Errorf(codes.OutOfRange, "invalid capacity range: %v", err)
	}

	volumeName := req.Name

	klog.Infof("volume_name: %s, size: %d, method: %s, volume_capabilities: %s", volumeName, size, "create_volume", req.GetVolumeCapabilities)

	klog.Info("create volume called")

	// get volume first, if it's created do no thing
	volumes, err := d.storage.List(ctx, &govpsie.ListOptions{
		Page:    1,
		PerPage: 1000,
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	for _, volume := range volumes {
		if volume.Name == volumeName {
			if int64(volume.Size)*giB != size {
				return nil, status.Error(codes.AlreadyExists, fmt.Sprintf("invalid option requested size: %d", size))
			}

			klog.Info("volume already created")
			return &csi.CreateVolumeResponse{
				Volume: &csi.Volume{
					VolumeId:      fmt.Sprint(volume.ID),
					CapacityBytes: int64(volume.Size) * giB,
				},
			}, nil
		}
	}

	createStorageRequest := &govpsie.StorageCreateRequest{
		Name:        volumeName,
		Size:        int(size) / giB,
		Description: createdByDO,
		StorageType: "EX4",
	}
	if d.config.StorageTag != "" {
		createStorageRequest.Tags = append(createStorageRequest.Tags, d.config.StorageTag)
	}

	klog.Infof("volume_req: %s\n , creating volume", createStorageRequest)
	err = d.storage.CreateStorage(ctx, createStorageRequest)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	vol := govpsie.Storage{}
	storages, err := d.storage.List(ctx, &govpsie.ListOptions{
		Page:    1,
		PerPage: 1000,
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	for _, storage := range storages {
		if storage.Name == volumeName {
			vol = storage
			break
		}
	}

	resp := &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      vol.Identifier,
			CapacityBytes: size,
			AccessibleTopology: []*csi.Topology{
				{
					Segments: map[string]string{
						"data-center": d.config.DataCenter,
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

	// check if volume exist before trying to attach it
	vol, err := d.storage.Get(ctx, req.VolumeId)
	if err != nil {
		return nil, err
	}

	// check if vpsie exist before trying to attach the volume to the droplet
	_, err = d.vpsie.Get(ctx, req.NodeId)
	if err != nil {
		return nil, err
	}

	attachedID := 0
	for _, id := range vol.DropletIDs {
		attachedID = id
		if id == dropletID {
			klog.Info("volume is already attached")
			return &csi.ControllerPublishVolumeResponse{
				PublishContext: map[string]string{
					d.publishInfoVolumeName: vol.Name,
				},
			}, nil
		}
	}

	// droplet is attached to a different node, return an error
	if attachedID != 0 {
		return nil, status.Errorf(codes.FailedPrecondition,
			"volume %q is attached to the wrong droplet (%d), detach the volume to fix it",
			req.VolumeId, attachedID)
	}

	// attach the volume to the correct node
	err := d.storageActions.Attach(ctx, req.VolumeId, dropletID)
	if err != nil {
		return nil, err
	}


	klog.Info("volume was attached")
	return &csi.ControllerPublishVolumeResponse{
		PublishContext: map[string]string{
			d.publishInfoVolumeName: vol.Name,
		},
	}, nil
}

func (d *Driver) ControllerUnpublishVolume(context.Context, *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ControllerUnpublishVolume not implemented")

}

func (d *Driver) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "ValidateVolumeCapabilities Volume ID must be provided")
	}

	if req.VolumeCapabilities == nil {
		return nil, status.Error(codes.InvalidArgument, "ValidateVolumeCapabilities Volume Capabilities must be provided")
	}


	klog.Infof("validate volume capabilities called, volume_id: %v, volume_capabilities: %v", req.VolumeId, req.VolumeCapabilities)

	// check if volume exist before trying to validate it it
	_, err := d.storage.Get(ctx, req.VolumeId)
	if err != nil {
		return nil, err
	}

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
	if maxEntries == 0 && d.defaultVolumesPageSize > 0 {
		maxEntries = int32(d.defaultVolumesPageSize)
	}

	klog.Info("list volumes called, max_entries: %d, req_starting_token %v", req.MaxEntries, req.StartingToken)

	var startingToken int32
	if req.StartingToken != "" {
		parsedToken, err := strconv.ParseInt(req.StartingToken, 10, 32)
		if err != nil {
			return nil, status.Errorf(codes.Aborted, "ListVolumes starting token %q is not valid: %s", req.StartingToken, err)
		}
		startingToken = int32(parsedToken)
	}

	untypedVolumes, nextToken, err := listResources(ctx, startingToken, maxEntries, func(ctx context.Context, listOpts *govpsie.ListOptions) ([]interface{}, error) {
	
		volumes, err := d.storage.List(ctx, listOpts)
		if err != nil {
			return nil, err
		}

		untypedVolumes := make([]interface{}, 0, len(volumes))
		for _, volume := range volumes {
			untypedVolumes = append(untypedVolumes, volume)
		}
		return untypedVolumes, err
	})
	if err != nil {
		return nil, fmt.Errorf("ListVolumes failed to list resources: %w", err)
	}

	volumes := make([]govpsie.Storage, 0, len(untypedVolumes))
	for _, untypedVolume := range untypedVolumes {
		volumes = append(volumes, untypedVolume.(govpsie.Storage))
	}

	var entries []*csi.ListVolumesResponse_Entry
	for _, vol := range volumes {
		attachedDropletIDs := make([]string, 0, len(vol.DropletIDs))
		for _, dropletID := range vol.DropletIDs {
			attachedDropletIDs = append(attachedDropletIDs, strconv.Itoa(dropletID))
		}

		entries = append(entries, &csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				VolumeId:      vol.ID,
				CapacityBytes: vol.SizeGigaBytes * giB,
			},
			Status: &csi.ListVolumesResponse_VolumeStatus{
				PublishedNodeIds: attachedDropletIDs,
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

func (d *Driver) ControllerGetCapabilities(context.Context, *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ControllerGetCapabilities not implemented")

}

func (d *Driver) CreateSnapshot(context.Context, *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateSnapshot not implemented")

}

func (d *Driver) DeleteSnapshot(context.Context, *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteSnapshot not implemented")

}

func (d *Driver) ListSnapshots(context.Context, *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListSnapshots not implemented")

}

func (d *Driver) ControllerExpandVolume(context.Context, *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ControllerExpandVolume not implemented")

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