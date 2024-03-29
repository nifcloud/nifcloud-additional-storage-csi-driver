package driver

import (
	"context"
	"errors"
	"strings"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/nifcloud/nifcloud-additional-storage-csi-driver/pkg/cloud"
	"github.com/nifcloud/nifcloud-additional-storage-csi-driver/pkg/driver/internal"
	"github.com/nifcloud/nifcloud-additional-storage-csi-driver/pkg/util"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
)

var (
	volumeCaps = []csi.VolumeCapability_AccessMode{
		{
			Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		},
	}

	controllerCaps = []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
		csi.ControllerServiceCapability_RPC_LIST_VOLUMES_PUBLISHED_NODES,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
	}
)

type controllerService struct {
	cloud      cloud.Cloud
	inFlight   *internal.InFlight
	instanceID string
}

func newControllerService(driverOptions *DriverOptions, instanceID string) controllerService {
	cloud, err := cloud.NewCloud(driverOptions.nifcloudSdkDebugLog)
	if err != nil {
		panic(err)
	}

	return controllerService{
		cloud:      cloud,
		instanceID: instanceID,
		inFlight:   internal.NewInFlight(),
	}
}

func (d *controllerService) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	klog.V(4).InfoS("CreateVolume: called", "args", *req)
	if err := validateCreateVolumeRequest(req); err != nil {
		return nil, err
	}

	volSizeBytes, err := getVolSizeBytes(req)
	if err != nil {
		return nil, err
	}

	volName := req.GetName()

	if ok := d.inFlight.Insert(volName); !ok {
		return nil, status.Errorf(codes.Aborted, "Create volume request for %s is already in progress", volName)
	}
	defer d.inFlight.Delete(volName)

	disk, err := d.cloud.GetDiskByName(ctx, volName, volSizeBytes)
	if err != nil {
		switch {
		case errors.Is(err, cloud.ErrNotFound):
		case errors.Is(err, cloud.ErrMultiDisks):
			return nil, status.Error(codes.Internal, err.Error())
		case errors.Is(err, cloud.ErrDiskExistsDiffSize):
			return nil, status.Error(codes.AlreadyExists, err.Error())
		default:
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	// volume already exists
	if disk != nil {
		return newCreateVolumeResponse(disk), nil
	}

	var (
		volumeType     string
		accountingType string
	)

	for key, value := range req.GetParameters() {
		switch strings.ToLower(key) {
		case "fstype":
			klog.InfoS("Deprecated \"fstype\" , please use \"csi.storage.k8s.io/fstype\" instead")
		case VolumeTypeKey:
			volumeType = value
		case AccountingTypeKey:
			accountingType = value
		default:
			return nil, status.Errorf(codes.InvalidArgument, "Invalid parameter key %s for CreateVolume", key)
		}
	}

	zone := pickAvailabilityZone(req.GetAccessibilityRequirements())
	if zone == "" {
		// use controller running zone as volume creation zone
		instance, err := d.cloud.GetInstanceByName(ctx, d.instanceID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Could not detect the zone to create disk: %v", err)
		}
		zone = instance.AvailabilityZone
	}
	klog.V(1).InfoS("Create volume", "zone", zone)

	// create a new volume
	opts := &cloud.DiskOptions{
		AccountingType: accountingType,
		CapacityBytes:  volSizeBytes,
		VolumeType:     volumeType,
		Zone:           zone,
	}

	disk, err = d.cloud.CreateDisk(ctx, volName, opts)
	if err != nil {
		errCode := codes.Internal
		if errors.Is(err, cloud.ErrNotFound) {
			errCode = codes.NotFound
		}
		return nil, status.Errorf(errCode, "Could not create volume %q: %v", volName, err)
	}

	return newCreateVolumeResponse(disk), nil
}

func validateCreateVolumeRequest(req *csi.CreateVolumeRequest) error {
	if len(req.GetName()) == 0 {
		return status.Error(codes.InvalidArgument, "Volume name not provided")
	}

	volCaps := req.GetVolumeCapabilities()
	if len(volCaps) == 0 {
		return status.Error(codes.InvalidArgument, "Volume capabilities not provided")
	}

	if !isValidVolumeCapabilities(volCaps) {
		return status.Error(codes.InvalidArgument, "Volume capabilities not supported")
	}
	return nil
}

func (d *controllerService) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	klog.V(4).InfoS("DeleteVolume: called", "args", *req)
	if err := validateDeleteVolumeRequest(req); err != nil {
		return nil, err
	}

	volumeID := req.GetVolumeId()

	if ok := d.inFlight.Insert(volumeID); !ok {
		return nil, status.Errorf(codes.Aborted, internal.VolumeOperationAlreadyExistsErrorMsg, volumeID)
	}
	defer d.inFlight.Delete(volumeID)

	if _, err := d.cloud.DeleteDisk(ctx, volumeID); err != nil {
		if errors.Is(err, cloud.ErrNotFound) {
			klog.V(4).InfoS("DeleteVolume: volume not found, returning with success")
			return &csi.DeleteVolumeResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal, "Could not delete volume ID %q: %v", volumeID, err)
	}

	return &csi.DeleteVolumeResponse{}, nil
}

func validateDeleteVolumeRequest(req *csi.DeleteVolumeRequest) error {
	if len(req.GetVolumeId()) == 0 {
		return status.Error(codes.InvalidArgument, "Volume ID not provided")
	}
	return nil
}

func (d *controllerService) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	klog.V(4).InfoS("ControllerPublishVolume: called", "args", *req)
	if err := validateControllerPublishVolumeRequest(req); err != nil {
		return nil, err
	}

	volumeID := req.GetVolumeId()
	nodeID := req.GetNodeId()

	if !d.cloud.IsExistInstance(ctx, nodeID) {
		return nil, status.Errorf(codes.NotFound, "Instance %q not found", nodeID)
	}

	if _, err := d.cloud.GetDiskByID(ctx, volumeID); err != nil {
		if errors.Is(err, cloud.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "Volume not found")
		}
		return nil, status.Errorf(codes.Internal, "Could not get volume with ID %q: %v", volumeID, err)
	}

	if ok := d.inFlight.Insert(volumeID); !ok {
		return nil, status.Errorf(codes.Aborted, internal.VolumeOperationAlreadyExistsErrorMsg, volumeID)
	}
	defer d.inFlight.Delete(volumeID)

	devicePath, err := d.cloud.AttachDisk(ctx, volumeID, nodeID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not attach volume %q to node %q: %v", volumeID, nodeID, err)
	}
	klog.V(5).InfoS("ControllerPublishVolume: attached", "volumeID", volumeID, "nodeID", nodeID, "devicePath", devicePath)

	pvInfo := map[string]string{DevicePathKey: devicePath}
	return &csi.ControllerPublishVolumeResponse{PublishContext: pvInfo}, nil
}

func validateControllerPublishVolumeRequest(req *csi.ControllerPublishVolumeRequest) error {
	if len(req.GetVolumeId()) == 0 {
		return status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	if len(req.GetNodeId()) == 0 {
		return status.Error(codes.InvalidArgument, "Node ID not provided")
	}

	volCap := req.GetVolumeCapability()
	if volCap == nil {
		return status.Error(codes.InvalidArgument, "Volume capability not provided")
	}

	caps := []*csi.VolumeCapability{volCap}
	if !isValidVolumeCapabilities(caps) {
		return status.Error(codes.InvalidArgument, "Volume capability not supported")
	}
	return nil
}

func (d *controllerService) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	klog.V(4).InfoS("ControllerUnpublishVolume: called", "args", *req)
	if err := validateControllerUnpublishVolumeRequest(req); err != nil {
		return nil, err
	}

	volumeID := req.GetVolumeId()
	nodeID := req.GetNodeId()

	if ok := d.inFlight.Insert(volumeID); !ok {
		return nil, status.Errorf(codes.Aborted, internal.VolumeOperationAlreadyExistsErrorMsg, volumeID)
	}
	defer d.inFlight.Delete(volumeID)

	if err := d.cloud.DetachDisk(ctx, volumeID, nodeID); err != nil {
		if errors.Is(err, cloud.ErrNotFound) {
			klog.V(5).InfoS("ControllerUnpublishVolume: volume not found", "volumeID", volumeID, "nodeID", nodeID)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal, "Could not detach volume %q from node %q: %v", volumeID, nodeID, err)
	}
	klog.V(5).InfoS("ControllerUnpublishVolume: detached", "volumeID", volumeID, "nodeID", nodeID)

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func validateControllerUnpublishVolumeRequest(req *csi.ControllerUnpublishVolumeRequest) error {
	if len(req.GetVolumeId()) == 0 {
		return status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	if len(req.GetNodeId()) == 0 {
		return status.Error(codes.InvalidArgument, "Node ID not provided")
	}

	return nil
}

func (d *controllerService) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	klog.V(4).InfoS("ControllerGetCapabilities: called", " args", *req)
	caps := make([]*csi.ControllerServiceCapability, len(controllerCaps))
	for _, cap := range controllerCaps {
		c := &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: cap,
				},
			},
		}
		caps = append(caps, c)
	}
	return &csi.ControllerGetCapabilitiesResponse{Capabilities: caps}, nil
}

func (d *controllerService) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	klog.V(4).InfoS("GetCapacity: called", "args", *req)
	return nil, status.Error(codes.Unimplemented, "")
}

func (d *controllerService) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	klog.V(4).InfoS("ListVolumes: called", "args", *req)
	disks, err := d.cloud.ListDisks(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not fetch the additional storage lists: %v", err)
	}

	entries := []*csi.ListVolumesResponse_Entry{}
	for _, d := range disks {
		entries = append(entries, &csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				VolumeId: d.VolumeID,
			},
			Status: &csi.ListVolumesResponse_VolumeStatus{
				PublishedNodeIds: []string{d.AttachedInstanceID},
			},
		})
	}

	return &csi.ListVolumesResponse{
		Entries: entries,
	}, nil
}

func (d *controllerService) ControllerGetVolume(ctx context.Context, req *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	klog.V(4).InfoS("ControllerGetVolume: called", "args", *req)
	if err := validateControllerGetVolumeRequest(req); err != nil {
		return nil, err
	}

	volumeID := req.GetVolumeId()

	disk, err := d.cloud.GetDiskByID(ctx, volumeID)
	if err != nil {
		if errors.Is(err, cloud.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "Volume not found")
		}
		return nil, status.Errorf(codes.Internal, "Could not get volume with ID %q: %v", volumeID, err)
	}

	return &csi.ControllerGetVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      disk.VolumeID,
			CapacityBytes: util.GiBToBytes(disk.CapacityGiB),
		},
		Status: &csi.ControllerGetVolumeResponse_VolumeStatus{
			PublishedNodeIds: []string{disk.AttachedInstanceID},
		},
	}, nil
}

func validateControllerGetVolumeRequest(req *csi.ControllerGetVolumeRequest) error {
	if len(req.GetVolumeId()) == 0 {
		return status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	return nil
}

func (d *controllerService) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	klog.V(4).InfoS("ValidateVolumeCapabilities: called", "args", *req)
	if err := validateValidateVolumeCapabilitiesRequest(req); err != nil {
		return nil, err
	}

	volumeID := req.GetVolumeId()
	volCaps := req.GetVolumeCapabilities()

	if _, err := d.cloud.GetDiskByID(ctx, volumeID); err != nil {
		if errors.Is(err, cloud.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "Volume not found")
		}
		return nil, status.Errorf(codes.Internal, "Could not get volume with ID %q: %v", volumeID, err)
	}

	var confirmed *csi.ValidateVolumeCapabilitiesResponse_Confirmed
	if isValidVolumeCapabilities(volCaps) {
		confirmed = &csi.ValidateVolumeCapabilitiesResponse_Confirmed{VolumeCapabilities: volCaps}
	}
	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: confirmed,
	}, nil
}

func validateValidateVolumeCapabilitiesRequest(req *csi.ValidateVolumeCapabilitiesRequest) error {
	if len(req.GetVolumeId()) == 0 {
		return status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	if len(req.GetVolumeCapabilities()) == 0 {
		return status.Error(codes.InvalidArgument, "Volume capabilities not provided")
	}

	return nil
}

func (d *controllerService) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	klog.V(4).InfoS("ControllerExpandVolume: called", "args", *req)
	if err := validateControllerExpandVolumeRequest(req); err != nil {
		return nil, err
	}

	volumeID := req.GetVolumeId()
	capRange := req.GetCapacityRange()

	newSize, err := util.RoundUpBytes(capRange.GetRequiredBytes())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "Failed to round-up volume size: %v", err)
	}

	maxVolSize := capRange.GetLimitBytes()
	if maxVolSize > 0 && maxVolSize < newSize {
		return nil, status.Error(codes.InvalidArgument, "After round-up, volume size exceeds the limit specified")
	}

	actualSizeGiB, err := d.cloud.ResizeDisk(ctx, volumeID, newSize)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not resize volume %q: %v", volumeID, err)
	}

	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         util.GiBToBytes(actualSizeGiB),
		NodeExpansionRequired: true,
	}, nil
}

func validateControllerExpandVolumeRequest(req *csi.ControllerExpandVolumeRequest) error {
	if len(req.GetVolumeId()) == 0 {
		return status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	if req.GetCapacityRange() == nil {
		return status.Error(codes.InvalidArgument, "Capacity range not provided")
	}

	return nil
}

func (d *controllerService) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	klog.V(4).InfoS("CreateSnapshot: called", "args", req)
	return nil, status.Error(codes.Unimplemented, "CreateSnapshot is not implemented (NIFCLOUD does not support the snapshot for volume)")
}

func (d *controllerService) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	klog.V(4).InfoS("DeleteSnapshot: called", "args", req)
	return nil, status.Error(codes.Unimplemented, "DeleteSnapshot is not implemented (NIFCLOUD does not support the snapshot for volume)")
}

func (d *controllerService) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	klog.V(4).InfoS("ListSnapshots: called", "args", req)
	return nil, status.Error(codes.Unimplemented, "ListSnapshots is not implemented (NIFCLOUD does not support the snapshot for volume)")
}

// pickAvailabilityZone selects 1 zone given topology requirement.
// if not found, empty string is returned.
func pickAvailabilityZone(requirement *csi.TopologyRequirement) string {
	if requirement == nil {
		return ""
	}
	for _, topology := range requirement.GetPreferred() {
		zone, exists := topology.GetSegments()[TopologyKey]
		if exists {
			return zone
		}
	}
	for _, topology := range requirement.GetRequisite() {
		zone, exists := topology.GetSegments()[TopologyKey]
		if exists {
			return zone
		}
	}
	return ""
}

func isValidVolumeCapabilities(volCaps []*csi.VolumeCapability) bool {
	hasSupport := func(cap *csi.VolumeCapability) bool {
		for _, c := range volumeCaps {
			if c.GetMode() == cap.AccessMode.GetMode() {
				return true
			}
		}
		return false
	}

	foundAll := true
	for _, c := range volCaps {
		if !hasSupport(c) {
			foundAll = false
		}
	}
	return foundAll
}

func newCreateVolumeResponse(disk *cloud.Disk) *csi.CreateVolumeResponse {
	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      disk.VolumeID,
			CapacityBytes: util.GiBToBytes(disk.CapacityGiB),
			VolumeContext: map[string]string{},
			AccessibleTopology: []*csi.Topology{
				{
					Segments: map[string]string{TopologyKey: disk.AvailabilityZone},
				},
			},
		},
	}
}

func getVolSizeBytes(req *csi.CreateVolumeRequest) (int64, error) {
	capRange := req.GetCapacityRange()
	if capRange == nil {
		return cloud.DefaultVolumeSize, nil
	}

	volSizeBytes, err := util.RoundUpBytes(capRange.GetRequiredBytes())
	if err != nil {
		return 0, status.Errorf(codes.InvalidArgument, "Failed to round-up volume size: %v", err)
	}
	maxVolSize := capRange.GetLimitBytes()
	if maxVolSize > 0 && maxVolSize < volSizeBytes {
		return 0, status.Error(codes.InvalidArgument, "After round-up, volume size exceeds the limit specified")
	}

	return volSizeBytes, nil
}
