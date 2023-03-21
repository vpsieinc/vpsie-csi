package driver

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"

	"k8s.io/mount-utils"
	// "k8s.io/utils/mount"

	utilexec "k8s.io/utils/exec"
)

const (
	defaultFsType     = FSTypeExt4
	maxVolumesPerNode = 7

	diskIDPath = "/dev/disk/by-id"
	diskPrefix = "scsi-0DO_Volume_"
)

var (
	ValidFSTypes = map[string]struct{}{
		FSTypeExt4: {},
		FSTypeXfs:  {},
		FSTypeNtfs: {},
	}
)

func (d *Driver) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeStageVolume Volume ID must be provided")
	}

	if req.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeStageVolume Staging Target Path must be provided")
	}

	if req.VolumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "NodeStageVolume Volume Capability must be provided")
	}

	klog.Info("node stage volume called")

	volumeName := ""
	if volName, ok := req.GetPublishContext()[d.publishInfoVolumeName]; !ok {
		return nil, status.Error(codes.InvalidArgument, "Could not find the volume by name")
	} else {
		volumeName = volName
	}

	// If it is a block volume, we do nothing for stage volume
	// because we bind mount the absolute device path to a file
	switch req.VolumeCapability.GetAccessType().(type) {
	case *csi.VolumeCapability_Block:
		return &csi.NodeStageVolumeResponse{}, nil
	}

	source := getDeviceByIDPath(volumeName)
	target := req.StagingTargetPath

	mnt := req.VolumeCapability.GetMount()
	options := mnt.MountFlags

	fsType := "ext4"
	if mnt.FsType != "" {
		fsType = mnt.FsType
	}

	_, noFormat := req.VolumeContext[noFormatKey]

	if noFormat {
		klog.Info("skipping formatting the source device")
	} else {

		formatted, err := d.mounter.IsFormatted(source)
		if err != nil {
			return nil, err
		}

		if !formatted {
			klog.Info("formatting the volume for staging")
			if err := d.mounter.Format(source, fsType); err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
		} else {
			klog.Info("source device is already formatted")
		}
	}

	klog.Info("mounting the volume for staging")

	mounted, err := d.mounter.IsMounted(target)
	if err != nil {
		return nil, err
	}

	if !mounted {
		if err := d.mounter.Mount(source, target, fsType, options...); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	} else {
		klog.Info("source device is already mounted to the target path")
	}

	klog.Info("formatting and mounting stage volume is finished")
	return &csi.NodeStageVolumeResponse{}, nil
}

func (d *Driver) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeUnstageVolume Volume ID must be provided")
	}

	if req.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeUnstageVolume Staging Target Path must be provided")
	}

	klog.Infof("node unstage volume called: volume_id: %s, staging_target_path: %s", req.VolumeId, req.StagingTargetPath)

	mounted, err := d.mounter.IsMounted(req.StagingTargetPath)
	if err != nil {
		return nil, err
	}

	if mounted {
		klog.Info("unmounting the staging target path")
		err := d.mounter.Unmount(req.StagingTargetPath)
		if err != nil {
			return nil, err
		}
	} else {
		klog.Info("staging target path is already unmounted")
	}

	klog.Info("unmounting stage volume is finished")
	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (d *Driver) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Volume ID must be provided")
	}

	if req.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Staging Target Path must be provided")
	}

	if req.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Target Path must be provided")
	}

	if req.VolumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Volume Capability must be provided")
	}

	klog.Info("node publish volume called")

	options := []string{"bind"}
	if req.Readonly {
		options = append(options, "ro")
	}

	var err error
	switch mode := req.GetVolumeCapability().GetAccessType().(type) {
	case *csi.VolumeCapability_Block:
		err = d.nodePublishVolumeForBlock(req, options)
	case *csi.VolumeCapability_Mount:
		err = d.nodePublishVolumeForFileSystem(req, options, mode)
	default:
		return nil, status.Error(codes.InvalidArgument, "Unknown access type")
	}

	if err != nil {
		return nil, err
	}

	klog.Info("bind mounting the volume is finished")
	return &csi.NodePublishVolumeResponse{}, nil
}

func (d *Driver) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeUnpublishVolume Volume ID must be provided")
	}

	if req.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeUnpublishVolume Target Path must be provided")
	}

	klog.Info("node unpublish volume called")

	err := d.mounter.Unmount(req.TargetPath)
	if err != nil {
		return nil, err
	}

	klog.Info("unmounting volume is finished")
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (d *Driver) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeGetVolumeStats Volume ID must be provided")
	}

	volumePath := req.VolumePath
	if volumePath == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeGetVolumeStats Volume Path must be provided")
	}

	klog.Info("node get volume stats called: volume_id: %v, path: %v", req.VolumeId, req.VolumePath)

	mounted, err := d.mounter.IsMounted(volumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to check if volume path %q is mounted: %s", volumePath, err)
	}

	if !mounted {
		return nil, status.Errorf(codes.NotFound, "volume path %q is not mounted", volumePath)
	}

	isBlock, err := d.mounter.IsBlockDevice(volumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to determine if %q is block device: %s", volumePath, err)
	}

	stats, err := d.mounter.GetStatistics(volumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to retrieve capacity statistics for volume path %q: %s", volumePath, err)
	}

	// only can retrieve total capacity for a block device
	if isBlock {
		klog.V(4).Infof("node capacity statistics retrieved bytes_total: %v", stats.totalBytes)

		return &csi.NodeGetVolumeStatsResponse{
			Usage: []*csi.VolumeUsage{
				{
					Unit:  csi.VolumeUsage_BYTES,
					Total: stats.totalBytes,
				},
			},
		}, nil
	}

	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			&csi.VolumeUsage{
				Available: stats.availableBytes,
				Total:     stats.totalBytes,
				Used:      stats.usedBytes,
				Unit:      csi.VolumeUsage_BYTES,
			},
			&csi.VolumeUsage{
				Available: stats.availableInodes,
				Total:     stats.totalInodes,
				Used:      stats.usedInodes,
				Unit:      csi.VolumeUsage_INODES,
			},
		},
	}, nil
}

func (d *Driver) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodeExpandVolume volume ID not provided")
	}

	volumePath := req.GetVolumePath()
	if len(volumePath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodeExpandVolume volume path not provided")
	}

	klog.Info("node expand volume called")

	if req.GetVolumeCapability() != nil {
		switch req.GetVolumeCapability().GetAccessType().(type) {
		case *csi.VolumeCapability_Block:
			klog.Info("filesystem expansion is skipped for block volumes")
			return &csi.NodeExpandVolumeResponse{}, nil
		}
	}

	mounted, err := d.mounter.IsMounted(volumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "NodeExpandVolume failed to check if volume path %q is mounted: %s", volumePath, err)
	}

	if !mounted {
		return nil, status.Errorf(codes.NotFound, "NodeExpandVolume volume path %q is not mounted", volumePath)
	}

	mounter := mount.New("")
	devicePath, err := d.mounter.GetDeviceName(mounter, volumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "NodeExpandVolume unable to get device path for %q: %v", volumePath, err)
	}

	if devicePath == "" {
		return nil, status.Errorf(codes.NotFound, "NodeExpandVolume device path for volume path %q not found", volumePath)
	}

	r := mount.NewResizeFs(utilexec.New())

	klog.Info("resizing volume")
	if _, err := r.Resize(devicePath, volumePath); err != nil {
		return nil, status.Errorf(codes.Internal, "NodeExpandVolume could not resize volume %q (%q):  %v", volumeID, req.GetVolumePath(), err)
	}

	klog.Info("volume was resized")
	return &csi.NodeExpandVolumeResponse{}, nil
}

func (d *Driver) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	nscaps := []*csi.NodeServiceCapability{
		&csi.NodeServiceCapability{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
				},
			},
		},
		&csi.NodeServiceCapability{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
				},
			},
		},
		&csi.NodeServiceCapability{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
				},
			},
		},
	}

	klog.Infof("node get capabilities called: node_capabilities: %v", nscaps)
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: nscaps,
	}, nil
}

func (d *Driver) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{
		NodeId:            "vmIdentifier",
		MaxVolumesPerNode: maxVolumesPerNode,

		// make sure that the driver works on this particular region only
		AccessibleTopology: &csi.Topology{
			Segments: map[string]string{
				"region": d.config.DataCenter,
			},
		},
	}, nil
}

func hasMountOption(options []string, opt string) bool {
	for _, o := range options {
		if o == opt {
			return true
		}
	}
	return false
}

func collectMountOptions(fsType string, mntFlags []string) []string {
	var options []string
	for _, opt := range mntFlags {
		if !hasMountOption(options, opt) {
			options = append(options, opt)
		}
	}

	// By default, xfs does not allow mounting of two volumes with the same filesystem uuid.
	// Force ignore this uuid to be able to mount volume + its clone / restored snapshot on the same node.
	if fsType == FSTypeXfs {
		if !hasMountOption(options, "nouuid") {
			options = append(options, "nouuid")
		}
	}
	return options
}

func getDeviceByIDPath(volumeName string) string {
	return filepath.Join(diskIDPath, fmt.Sprintf("%s%s", diskPrefix, volumeName))
}

func (d *Driver) nodePublishVolumeForFileSystem(req *csi.NodePublishVolumeRequest, mountOptions []string, mode *csi.VolumeCapability_Mount) error {
	source := req.StagingTargetPath
	target := req.TargetPath

	mnt := req.VolumeCapability.GetMount()
	for _, flag := range mnt.MountFlags {
		mountOptions = append(mountOptions, flag)
	}

	mounted, err := d.mounter.IsMounted(target)
	if err != nil {
		return err
	}

	if !mounted {
		fsType := mode.Mount.GetFsType()
		if len(fsType) == 0 {
			fsType = defaultFsType
		}

		_, ok := ValidFSTypes[strings.ToLower(fsType)]
		if !ok {
			return status.Errorf(codes.InvalidArgument, "NodePublishVolume: invalid fstype %s", fsType)
		}

		klog.Info("mounting the volume")
		if err := d.mounter.Mount(source, target, fsType, mountOptions...); err != nil {
			return status.Error(codes.Internal, err.Error())
		}
	} else {
		klog.Info("volume is already mounted")
	}

	return nil
}

func (d *Driver) nodePublishVolumeForBlock(req *csi.NodePublishVolumeRequest, mountOptions []string) error {
	volumeName, ok := req.GetPublishContext()[d.publishInfoVolumeName]
	if !ok {
		return status.Error(codes.InvalidArgument, fmt.Sprintf("Could not find the volume name from the publish context %q", d.publishInfoVolumeName))
	}

	source, err := findAbsoluteDeviceByIDPath(volumeName)
	if err != nil {
		return status.Errorf(codes.Internal, "Failed to find device path for volume %s. %v", volumeName, err)
	}

	target := req.TargetPath

	mounted, err := d.mounter.IsMounted(target)
	if err != nil {
		return err
	}

	if !mounted {
		klog.Info("mounting the volume")
		if err := d.mounter.Mount(source, target, "", mountOptions...); err != nil {
			return status.Errorf(codes.Internal, err.Error())
		}
	} else {
		klog.Info("volume is already mounted")
	}

	return nil
}

func findAbsoluteDeviceByIDPath(volumeID string) (string, error) {
	path := getDeviceByIDPath(volumeID)

	// EvalSymlinks returns relative link if the file is not a symlink
	// so we do not have to check if it is symlink prior to evaluation
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("could not resolve symlink %q: %v", path, err)
	}

	if !strings.HasPrefix(resolved, "/dev") {
		return "", fmt.Errorf("resolved symlink %q for %q was unexpected", resolved, path)
	}

	return resolved, nil
}
