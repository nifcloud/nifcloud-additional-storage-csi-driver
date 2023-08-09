package driver

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/aokumasan/nifcloud-additional-storage-csi-driver/pkg/cloud"
	"github.com/aokumasan/nifcloud-additional-storage-csi-driver/pkg/driver/internal"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	"k8s.io/mount-utils"
	"k8s.io/utils/exec"
)

const (
	// FSTypeExt2 represents the ext2 filesystem type
	FSTypeExt2 = "ext2"
	// FSTypeExt3 represents the ext3 filesystem type
	FSTypeExt3 = "ext3"
	// FSTypeExt4 represents the ext4 filesystem type
	FSTypeExt4 = "ext4"
	// FSTypeXfs represents te xfs filesystem type
	FSTypeXfs = "xfs"

	// default file system type to be used when it is not provided
	defaultFsType = FSTypeExt4

	// defaultMaxVolumes is the maximum number of volumes that an NIFCLOUD instance can have attached.
	// More info at https://pfs.nifcloud.com/service/disk.htm
	defaultMaxVolumes = 14
)

var (
	// ValidFSTypes is valid filesystem type.
	ValidFSTypes = []string{FSTypeExt2, FSTypeExt3, FSTypeExt4, FSTypeXfs}
)

var (
	// nodeCaps represents the capability of node service.
	nodeCaps = []csi.NodeServiceCapability_RPC_Type{
		csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
		csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
		csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
	}
)

// nodeService represents the node service of CSI driver
type nodeService struct {
	cloud      cloud.Cloud
	mounter    Mounter
	inFlight   *internal.InFlight
	instanceID string
}

// newNodeService creates a new node service
// it panics if failed to create the service
func newNodeService(instanceID string) nodeService {
	cloud, err := cloud.NewCloud()
	if err != nil {
		panic(err)
	}

	return nodeService{
		cloud:      cloud,
		mounter:    newNodeMounter(),
		inFlight:   internal.NewInFlight(),
		instanceID: instanceID,
	}
}

func (n *nodeService) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	klog.V(4).Infof("NodeStageVolume: called with args: %+v", *req)

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	target := req.GetStagingTargetPath()
	if len(target) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Staging target not provided")
	}

	volCap := req.GetVolumeCapability()
	if volCap == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability not provided")
	}

	if !isValidVolumeCapabilities([]*csi.VolumeCapability{volCap}) {
		return nil, status.Errorf(codes.InvalidArgument, "Volume capability not supported: %v", volCap)
	}

	if err := n.scanStorageDevices(); err != nil {
		return nil, status.Errorf(codes.Internal, "Could not scan the SCSI storages: %v", err)
	}

	switch volCap.GetAccessType().(type) {
	case *csi.VolumeCapability_Block:
		return &csi.NodeStageVolumeResponse{}, nil
	}

	mount := volCap.GetMount()
	if mount == nil {
		return nil, status.Error(codes.InvalidArgument, "NodeStageVolume: mount is nil within volume capability")
	}
	fsType := mount.GetFsType()
	if len(fsType) == 0 {
		fsType = defaultFsType
	}

	if ok := n.inFlight.Insert(volumeID); !ok {
		return nil, status.Errorf(codes.Aborted, internal.VolumeOperationAlreadyExistsErrorMsg, volumeID)
	}
	defer func() {
		klog.V(4).InfoS("NodeStageVolume: volume operation finished", "volumeID", volumeID)
		n.inFlight.Delete(volumeID)
	}()

	devicePath, ok := req.PublishContext[DevicePathKey]
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "Device path not provided")
	}

	source, err := n.findDevicePath(devicePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to find device path %s: %v", devicePath, err)
	}

	klog.V(4).Infof("NodeStageVolume: find device path %s -> %s", devicePath, source)

	exists, err := n.mounter.ExistsPath(target)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to check if target %q exists: %v", target, err)
	}

	if !exists {
		klog.V(4).Infof("NodeStageVolume: creating target dir %q", target)
		if err = n.mounter.MakeDir(target); err != nil {
			return nil, status.Errorf(codes.Internal, "could not create target dir %q: %v", target, err)
		}
	}

	device, _, err := n.mounter.GetDeviceName(target)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to check if volume is already mounted: %v", err)
	}

	if device == source {
		klog.V(4).Infof("NodeStageVolume: volume=%q already staged", volumeID)
		return &csi.NodeStageVolumeResponse{}, nil
	}

	klog.V(5).Infof("NodeStageVolume: formatting %s and mounting at %s with fstype %s", source, target, fsType)
	err = n.mounter.FormatAndMount(source, target, fsType, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not format %q and mount it at %q", source, target)
	}

	return &csi.NodeStageVolumeResponse{}, nil

}

func (n *nodeService) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	klog.V(4).Infof("NodeUnstageVolume: called with args %+v", *req)
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	target := req.GetStagingTargetPath()
	if len(target) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Staging target not provided")
	}

	dev, refCount, err := n.mounter.GetDeviceName(target)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to check if volume is mounted: %v", err)
	}

	if refCount == 0 {
		klog.V(5).Infof("NodeUnstageVolume: %s target not mounted", target)
		return &csi.NodeUnstageVolumeResponse{}, nil
	}

	if refCount > 1 {
		klog.Warningf("NodeUnstageVolume: found %d references to device %s mounted at target path %s", refCount, dev, target)
	}

	if ok := n.inFlight.Insert(volumeID); !ok {
		return nil, status.Errorf(codes.Aborted, internal.VolumeOperationAlreadyExistsErrorMsg, volumeID)
	}
	defer func() {
		klog.V(4).InfoS("NodeUnStageVolume: volume operation finished", "volumeID", volumeID)
		n.inFlight.Delete(volumeID)
	}()

	klog.V(5).Infof("NodeUnstageVolume: unmounting %s", target)
	err = n.mounter.Unmount(target)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not unmount target %q: %v", target, err)
	}

	// remove storage device
	klog.Infof("removing storage device of %q", dev)
	if err := n.removeStorageDevice(dev); err != nil {
		return nil, status.Errorf(codes.Internal, "Could not remove the storage device: %v", err)
	}

	return &csi.NodeUnstageVolumeResponse{}, nil

}

func (n *nodeService) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	klog.V(4).Infof("NodeExpandVolume: called with args %+v", *req)
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	args := []string{"-o", "source", "--noheadings", "--target", req.GetVolumePath()}
	output, err := n.mounter.Command("findmnt", args...).Output()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not determine device path: %v", err)
	}

	devicePath := strings.TrimSpace(string(output))
	if len(devicePath) == 0 {
		return nil, status.Errorf(codes.Internal, "Could not get valid device for mount path: %q", req.GetVolumePath())
	}

	if err := n.rescanStorageDevice(devicePath); err != nil {
		return nil, status.Errorf(codes.Internal, "Could not rescan the device of %s: %v", devicePath, err)
	}

	r := mount.NewResizeFs(exec.New())
	if _, err := r.Resize(devicePath, req.GetVolumePath()); err != nil {
		return nil, status.Errorf(codes.Internal, "Could not resize volume %q (%q): %v", volumeID, devicePath, err)
	}

	return &csi.NodeExpandVolumeResponse{}, nil
}

func (n *nodeService) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	klog.V(4).Infof("NodePublishVolume: called with args %+v", *req)
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	source := req.GetStagingTargetPath()
	if len(source) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Staging target not provided")
	}

	target := req.GetTargetPath()
	if len(target) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path not provided")
	}

	volCap := req.GetVolumeCapability()
	if volCap == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability not provided")
	}

	if !isValidVolumeCapabilities([]*csi.VolumeCapability{volCap}) {
		return nil, status.Errorf(codes.InvalidArgument, "Volume capability not supported: %v", volCap)
	}

	if ok := n.inFlight.Insert(volumeID); !ok {
		return nil, status.Errorf(codes.Aborted, internal.VolumeOperationAlreadyExistsErrorMsg, volumeID)
	}
	defer func() {
		klog.V(4).InfoS("NodePublishVolume: volume operation finished", "volumeId", volumeID)
		n.inFlight.Delete(volumeID)
	}()

	mountOptions := []string{"bind"}
	if req.GetReadonly() {
		mountOptions = append(mountOptions, "ro")
	}

	switch mode := volCap.GetAccessType().(type) {
	case *csi.VolumeCapability_Block:
		if err := n.nodePublishVolumeForBlock(req, mountOptions); err != nil {
			return nil, err
		}
	case *csi.VolumeCapability_Mount:
		if err := n.nodePublishVolumeForFileSystem(req, mountOptions, mode); err != nil {
			return nil, err
		}
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (n *nodeService) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	klog.V(4).Infof("NodeUnpublishVolume: called with args %+v", *req)
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	target := req.GetTargetPath()
	if len(target) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path not provided")
	}

	if ok := n.inFlight.Insert(volumeID); !ok {
		return nil, status.Errorf(codes.Aborted, internal.VolumeOperationAlreadyExistsErrorMsg, volumeID)
	}
	defer func() {
		klog.V(4).InfoS("NodeUnPublishVolume: volume operation finished", "volumeId", volumeID)
		n.inFlight.Delete(volumeID)
	}()

	klog.V(5).Infof("NodeUnpublishVolume: unmounting %s", target)
	err := n.mounter.Unmount(target)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not unmount %q: %v", target, err)
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (n *nodeService) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	klog.V(4).Infof("NodeGetVolumeStats: called with args %+v", *req)
	if len(req.VolumeId) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodeGetVolumeStats volume ID was empty")
	}
	if len(req.VolumePath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodeGetVolumeStats volume path was empty")
	}

	exists, err := n.mounter.ExistsPath(req.VolumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "unknown error when stat on %s: %v", req.VolumePath, err)
	}
	if !exists {
		return nil, status.Errorf(codes.NotFound, "path %s does not exist", req.VolumePath)
	}

	isBlock, err := isBlockDevice(req.VolumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to determine whether %s is block device: %v", req.VolumePath, err)
	}

	if isBlock {
		bcap, err := n.getBlockSizeBytes(req.VolumePath)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get block capacity on path %s: %v", req.VolumePath, err)
		}

		return &csi.NodeGetVolumeStatsResponse{
			Usage: []*csi.VolumeUsage{
				{
					Unit:  csi.VolumeUsage_BYTES,
					Total: bcap,
				},
			},
		}, nil
	}

	available, capacity, used, inodes, inodesFree, inodesUsed, err := getFsInfo(req.VolumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get FsInfo due to error: %v", err)
	}

	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{
				Unit:      csi.VolumeUsage_BYTES,
				Available: resource.NewQuantity(available, resource.BinarySI).AsDec().UnscaledBig().Int64(),
				Total:     resource.NewQuantity(capacity, resource.BinarySI).AsDec().UnscaledBig().Int64(),
				Used:      resource.NewQuantity(used, resource.BinarySI).AsDec().UnscaledBig().Int64(),
			},
			{
				Unit:      csi.VolumeUsage_INODES,
				Available: resource.NewQuantity(inodesFree, resource.BinarySI).AsDec().UnscaledBig().Int64(),
				Total:     resource.NewQuantity(inodes, resource.BinarySI).AsDec().UnscaledBig().Int64(),
				Used:      resource.NewQuantity(inodesUsed, resource.BinarySI).AsDec().UnscaledBig().Int64(),
			},
		},
	}, nil
}

func (n *nodeService) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	klog.V(4).Infof("NodeGetCapabilities: called with args %+v", *req)
	var caps []*csi.NodeServiceCapability
	for _, cap := range nodeCaps {
		c := &csi.NodeServiceCapability{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: cap,
				},
			},
		}
		caps = append(caps, c)
	}
	return &csi.NodeGetCapabilitiesResponse{Capabilities: caps}, nil
}

func (n *nodeService) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	klog.V(4).Infof("NodeGetInfo: called with args %+v", *req)
	zone := os.Getenv("NIFCLOUD_ZONE")
	if zone == "" {
		instance, err := n.cloud.GetInstanceByName(ctx, n.instanceID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to get instance info for %q: %v", n.instanceID, err)
		}
		zone = instance.AvailabilityZone
	}

	topology := &csi.Topology{
		Segments: map[string]string{TopologyKey: zone},
	}

	return &csi.NodeGetInfoResponse{
		NodeId:             n.instanceID,
		MaxVolumesPerNode:  defaultMaxVolumes,
		AccessibleTopology: topology,
	}, nil
}

func (n *nodeService) nodePublishVolumeForBlock(req *csi.NodePublishVolumeRequest, mountOptions []string) error {
	target := req.GetTargetPath()

	devicePath, exists := req.PublishContext[DevicePathKey]
	if !exists {
		return status.Error(codes.InvalidArgument, "Device path not provided")
	}
	source, err := n.findDevicePath(devicePath)
	if err != nil {
		return status.Errorf(codes.Internal, "Failed to find device path %s. %v", devicePath, err)
	}

	klog.V(4).Infof("NodePublishVolume [block]: find device path %s -> %s", devicePath, source)

	globalMountPath := filepath.Dir(target)

	// create the global mount path if it is missing
	// Path in the form of /var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/publish/{volumeName}
	exists, err = n.mounter.ExistsPath(globalMountPath)
	if err != nil {
		return status.Errorf(codes.Internal, "Could not check if path exists %q: %v", globalMountPath, err)
	}

	if !exists {
		if err := n.mounter.MakeDir(globalMountPath); err != nil {
			return status.Errorf(codes.Internal, "Could not create dir %q: %v", globalMountPath, err)
		}
	}

	// Create the mount point as a file since bind mount device node requires it to be a file
	klog.V(5).Infof("NodePublishVolume [block]: making target file %s", target)
	err = n.mounter.MakeFile(target)
	if err != nil {
		if removeErr := os.Remove(target); removeErr != nil {
			return status.Errorf(codes.Internal, "Could not remove mount target %q: %v", target, removeErr)
		}
		return status.Errorf(codes.Internal, "Could not create file %q: %v", target, err)
	}

	klog.V(5).Infof("NodePublishVolume [block]: mounting %s at %s", source, target)
	if err := n.mounter.Mount(source, target, "", mountOptions); err != nil {
		if removeErr := os.Remove(target); removeErr != nil {
			return status.Errorf(codes.Internal, "Could not remove mount target %q: %v", target, removeErr)
		}
		return status.Errorf(codes.Internal, "Could not mount %q at %q: %v", source, target, err)
	}

	return nil
}

func (n *nodeService) nodePublishVolumeForFileSystem(req *csi.NodePublishVolumeRequest, mountOptions []string, mode *csi.VolumeCapability_Mount) error {
	target := req.GetTargetPath()
	source := req.GetStagingTargetPath()
	if m := mode.Mount; m != nil {
		hasOption := func(options []string, opt string) bool {
			for _, o := range options {
				if o == opt {
					return true
				}
			}
			return false
		}
		for _, f := range m.MountFlags {
			if !hasOption(mountOptions, f) {
				mountOptions = append(mountOptions, f)
			}
		}
	}

	klog.V(5).Infof("NodePublishVolume: creating dir %s", target)
	if err := n.mounter.MakeDir(target); err != nil {
		return status.Errorf(codes.Internal, "Could not create dir %q: %v", target, err)
	}

	fsType := mode.Mount.GetFsType()
	if len(fsType) == 0 {
		fsType = defaultFsType
	}

	klog.V(5).Infof("NodePublishVolume: mounting %s at %s with option %s as fstype %s", source, target, mountOptions, fsType)
	if err := n.mounter.Mount(source, target, fsType, mountOptions); err != nil {
		if removeErr := os.Remove(target); removeErr != nil {
			return status.Errorf(codes.Internal, "Could not remove mount target %q: %v", target, err)
		}
		return status.Errorf(codes.Internal, "Could not mount %q at %q: %v", source, target, err)
	}

	return nil
}

func (n *nodeService) getBlockSizeBytes(devicePath string) (int64, error) {
	cmd := n.mounter.(*NodeMounter).Exec.Command("blockdev", "--getsize64", devicePath)
	output, err := cmd.Output()
	if err != nil {
		return -1, fmt.Errorf("error when getting size of block volume at path %s: output: %s, err: %v", devicePath, string(output), err)
	}

	strOut := strings.TrimSpace(string(output))
	gotSizeBytes, err := strconv.ParseInt(strOut, 10, 64)
	if err != nil {
		return -1, fmt.Errorf("failed to parse size %s as int", strOut)
	}

	return gotSizeBytes, nil
}

func (n *nodeService) findDevicePath(scsiID string) (string, error) {
	if !strings.HasPrefix(scsiID, "SCSI") {
		return "", fmt.Errorf("invalid SCSI ID %q was specified. SCSI ID must be start with SCSI (0:?)", scsiID)
	}
	deviceNumberRegexp := regexp.MustCompile(`SCSI\s\(0:(.+)\)$`)
	match := deviceNumberRegexp.FindSubmatch([]byte(scsiID))
	if match == nil {
		return "", fmt.Errorf("could not detect device file from SCSI id %q", scsiID)
	}
	deviceNumber := string(match[1])

	deviceFileDir := "/dev/disk/by-path"
	files, err := ioutil.ReadDir(deviceFileDir)
	if err != nil {
		return "", fmt.Errorf("could not list the files in /dev/disk/by-path/: %v", err)
	}

	devicePath := ""
	deviceFileRegexp := regexp.MustCompile(fmt.Sprintf(`^pci-\d{4}:\d{2}:\d{2}\.\d-scsi-0:0:%s:0$`, deviceNumber))
	for _, f := range files {
		if deviceFileRegexp.MatchString(f.Name()) {
			devicePath, err = filepath.EvalSymlinks(filepath.Join(deviceFileDir, f.Name()))
			if err != nil {
				return "", fmt.Errorf("could not eval symlynk for %q: %v", f.Name(), err)
			}
		}
	}

	if devicePath == "" {
		return "", fmt.Errorf("could not find device file from SCSI ID %q", scsiID)
	}

	exists, err := n.mounter.ExistsPath(devicePath)
	if err != nil {
		return "", err
	}

	if exists {
		return devicePath, nil
	}

	return "", fmt.Errorf("device path not found: %s", devicePath)
}

// scanStorageDevices online scan the new storage device
// More info: https://pfs.nifcloud.com/guide/cp/login/mount_linux.htm
func (n *nodeService) scanStorageDevices() error {
	scanTargets := []string{"/sys/class/scsi_host", "/sys/devices"}
	for _, target := range scanTargets {
		err := filepath.Walk(target, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if info.IsDir() {
				return nil
			}

			if info.Name() != "scan" {
				return nil
			}

			f, err := os.OpenFile(path, os.O_WRONLY, os.ModePerm)
			if err != nil {
				return err
			}
			defer f.Close()

			_, err = fmt.Fprint(f, "- - -")
			if err != nil {
				return err
			}

			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to scan devices: %v", err)
		}
	}

	return nil
}

// removeStorageDevice online detach the specified storage
// More info: https://pfs.nifcloud.com/guide/cp/login/detach_linux.htm
func (n *nodeService) removeStorageDevice(dev string) error {
	removeDevicePath := filepath.Join("/sys/block/", filepath.Base(dev), "/device/delete")
	if _, err := os.Stat(removeDevicePath); err != nil {
		// If the path does not exist, assume it is removed from this node
		return nil
	}

	f, err := os.OpenFile(removeDevicePath, os.O_WRONLY, os.ModePerm)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprint(f, "1")
	if err != nil {
		return err
	}

	return nil
}

// rescanStorageDevice online rescan the specified storage
// More info: https://pfs.nifcloud.com/guide/cp/login/extend_partition_linux.htm
func (n *nodeService) rescanStorageDevice(dev string) error {
	rescanDevicePath := filepath.Join("/sys/block/", filepath.Base(dev), "/device/rescan")
	if _, err := os.Stat(rescanDevicePath); err != nil {
		return fmt.Errorf("target device %q not found in /sys/block: %w", dev, err)
	}

	f, err := os.OpenFile(rescanDevicePath, os.O_WRONLY, os.ModePerm)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := fmt.Fprint(f, "1"); err != nil {
		return err
	}

	return nil
}

func getFsInfo(path string) (int64, int64, int64, int64, int64, int64, error) {
	statfs := &unix.Statfs_t{}
	err := unix.Statfs(path, statfs)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}

	// Available is blocks available * fragment size
	available := int64(statfs.Bavail) * int64(statfs.Bsize)

	// Capacity is total block count * fragment size
	capacity := int64(statfs.Blocks) * int64(statfs.Bsize)

	// Usage is block being used * fragment size (aka block size).
	usage := (int64(statfs.Blocks) - int64(statfs.Bfree)) * int64(statfs.Bsize)

	inodes := int64(statfs.Files)
	inodesFree := int64(statfs.Ffree)
	inodesUsed := inodes - inodesFree

	return available, capacity, usage, inodes, inodesFree, inodesUsed, nil
}

func isBlockDevice(fullPath string) (bool, error) {
	var st unix.Stat_t
	err := unix.Stat(fullPath, &st)
	if err != nil {
		return false, err
	}

	return (st.Mode & unix.S_IFMT) == unix.S_IFBLK, nil
}
