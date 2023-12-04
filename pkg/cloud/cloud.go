package cloud

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/smithy-go"
	"github.com/nifcloud/nifcloud-additional-storage-csi-driver/pkg/util"
	"github.com/nifcloud/nifcloud-sdk-go/nifcloud"
	"github.com/nifcloud/nifcloud-sdk-go/service/computing"
	"github.com/nifcloud/nifcloud-sdk-go/service/computing/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

const (
	// Available disk types
	// doc: https://pfs.nifcloud.com/service/disk.htm

	// VolumeTypeStandard represents a general purpose volume.
	VolumeTypeStandard = "standard"
	// VolumeTypeHighSpeed represents a high spped volume (randomly select type A or B)
	VolumeTypeHighSpeed = "high-speed"
	// VolumeTypeHighSpeedA represents a high speed volume (only use type A).
	VolumeTypeHighSpeedA = "high-speed-a"
	// VolumeTypeHighSpeedB represents a high speed volume (only use type B).
	VolumeTypeHighSpeedB = "high-speed-b"
	// VolumeTypeFlash represents a flash volume.
	VolumeTypeFlash = "flash"
	// VolumeTypeStandardFlash represents a standard flash volume (randomly select type A or B)
	VolumeTypeStandardFlash = "standard-flash"
	// VolumeTypeStandardFlashA represents a standard flash volume (only use type A)
	VolumeTypeStandardFlashA = "standard-flash-a"
	// VolumeTypeStandardFlashB represents a standard flash volume (only use type B)
	VolumeTypeStandardFlashB = "standard-flash-b"
	// VolumeTypeHighSpeedFlash represents a high spped flash volume (randomly select type A or B)
	VolumeTypeHighSpeedFlash = "high-speed-flash"
	// VolumeTypeHighSpeedFlashA represents a high spped flash volume (only use type A)
	VolumeTypeHighSpeedFlashA = "high-speed-flash-a"
	// VolumeTypeHighSpeedFlashB represents a high spped flash volume (only use type B)
	VolumeTypeHighSpeedFlashB = "high-speed-flash-b"
)

var (
	// VolumeTypeMapping converts the volume identifier from volume type.
	// More info: https://pfs.nifcloud.com/api/rest/CreateVolume.htm
	VolumeTypeMapping = map[string]types.DiskTypeOfCreateVolumeRequest{
		VolumeTypeStandard:        types.DiskTypeOfCreateVolumeRequestStandardStorage,
		VolumeTypeHighSpeedA:      types.DiskTypeOfCreateVolumeRequestHighSpeedStorageA,
		VolumeTypeHighSpeedB:      types.DiskTypeOfCreateVolumeRequestHighSpeedStorageB,
		VolumeTypeFlash:           types.DiskTypeOfCreateVolumeRequestFlashDrive,
		VolumeTypeStandardFlashA:  types.DiskTypeOfCreateVolumeRequestStandardFlashStorageA,
		VolumeTypeStandardFlashB:  types.DiskTypeOfCreateVolumeRequestStandardFlashStorageB,
		VolumeTypeHighSpeedFlashA: types.DiskTypeOfCreateVolumeRequestHighSpeedFlashStorageA,
		VolumeTypeHighSpeedFlashB: types.DiskTypeOfCreateVolumeRequestHighSpeedFlashStorageB,
	}
)

const (
	// DefaultVolumeSize represents the default volume size.
	DefaultVolumeSize int64 = 100 * util.GiB
	// DefaultVolumeType specifies which storage to use for newly created volumes.
	DefaultVolumeType = VolumeTypeStandardFlashA
)

var (
	// ErrMultiDisks is an error that is returned when multiple
	// disks are found with the same volume name.
	ErrMultiDisks = errors.New("Multiple disks with same name")

	// ErrDiskExistsDiffSize is an error that is returned if a disk with a given
	// name, but different size, is found.
	ErrDiskExistsDiffSize = errors.New("There is already a disk with same name and different size")

	// ErrNotFound is returned when a resource is not found.
	ErrNotFound = errors.New("Resource was not found")

	// ErrAlreadyExists is returned when a resource is already existent.
	ErrAlreadyExists = errors.New("Resource already exists")

	// ErrInvalidMaxResults is returned when a MaxResults pagination parameter is between 1 and 4
	ErrInvalidMaxResults = errors.New("MaxResults parameter must be 0 or greater than or equal to 5")
)

// Disk represents a NIFCLOUD additional storage
type Disk struct {
	VolumeID           string
	CapacityGiB        int64
	AvailabilityZone   string
	AttachedInstanceID string
}

// DiskOptions represents parameters to create an NIFCLOUD additional storage
type DiskOptions struct {
	CapacityBytes int64
	VolumeType    string
	Zone          string
}

// Instance represents a NIFCLOUD VM
type Instance struct {
	InstanceID       string
	AvailabilityZone string
}

// Cloud is interface for cloud api manipulator
type Cloud interface {
	CreateDisk(ctx context.Context, volumeName string, diskOptions *DiskOptions) (disk *Disk, err error)
	DeleteDisk(ctx context.Context, volumeID string) (success bool, err error)
	AttachDisk(ctx context.Context, volumeID string, nodeID string) (devicePath string, err error)
	DetachDisk(ctx context.Context, volumeID string, nodeID string) (err error)
	ResizeDisk(ctx context.Context, volumeID string, size int64) (newSize int64, err error)
	ListDisks(ctx context.Context) (disks []*Disk, err error)
	WaitForAttachmentState(ctx context.Context, volumeID, state string) error
	GetDiskByName(ctx context.Context, name string, capacityBytes int64) (disk *Disk, err error)
	GetDiskByID(ctx context.Context, volumeID string) (disk *Disk, err error)
	IsExistInstance(ctx context.Context, nodeID string) (success bool)
	GetInstanceByName(ctx context.Context, name string) (*Instance, error)
}

type cloud struct {
	region    string
	computing *computing.Client
}

var _ Cloud = &cloud{}

// NewCloud creates the cloud object.
func NewCloud() (Cloud, error) {
	accessKeyID := os.Getenv("NIFCLOUD_ACCESS_KEY_ID")
	secretAccessKey := os.Getenv("NIFCLOUD_SECRET_ACCESS_KEY")
	region := os.Getenv("NIFCLOUD_REGION")
	cfg := nifcloud.NewConfig(accessKeyID, secretAccessKey, region)
	return &cloud{
		region:    region,
		computing: computing.NewFromConfig(cfg),
	}, nil
}

func (c *cloud) CreateDisk(ctx context.Context, volumeName string, diskOptions *DiskOptions) (*Disk, error) {
	var createType types.DiskTypeOfCreateVolumeRequest
	switch diskOptions.VolumeType {
	case VolumeTypeStandard, VolumeTypeHighSpeedA, VolumeTypeHighSpeedB, VolumeTypeFlash, VolumeTypeStandardFlashA, VolumeTypeStandardFlashB, VolumeTypeHighSpeedFlashA, VolumeTypeHighSpeedFlashB:
		createType = VolumeTypeMapping[diskOptions.VolumeType]
	case VolumeTypeHighSpeed:
		types := []string{VolumeTypeHighSpeedA, VolumeTypeHighSpeedB}
		createType = VolumeTypeMapping[types[rand.Intn(len(types))]] //nolint:gosec // Strong random is unnecessary
	case VolumeTypeStandardFlash:
		types := []string{VolumeTypeStandardFlashA, VolumeTypeStandardFlashB}
		createType = VolumeTypeMapping[types[rand.Intn(len(types))]] //nolint:gosec // Strong random is unnecessary
	case VolumeTypeHighSpeedFlash:
		types := []string{VolumeTypeHighSpeedFlashA, VolumeTypeHighSpeedFlashB}
		createType = VolumeTypeMapping[types[rand.Intn(len(types))]] //nolint:gosec // Strong random is unnecessary
	case "":
		createType = VolumeTypeMapping[DefaultVolumeType]
	default:
		return nil, fmt.Errorf("invalid NIFCLOUD VolumeType %q", diskOptions.VolumeType)
	}

	zone := diskOptions.Zone
	if zone == "" {
		return nil, errors.New("Zone is required")
	}

	instances, err := c.listInstancesByZone(ctx, zone)
	if err != nil {
		return nil, err
	}
	instanceID := instances[0].InstanceID

	capacity := roundUpCapacity(util.BytesToGiB(diskOptions.CapacityBytes))

	input := &computing.CreateVolumeInput{
		AccountingType: types.AccountingTypeOfCreateVolumeRequestHourly, // TODO: set accounting type from diskoptions
		DiskType:       createType,
		InstanceId:     nifcloud.String(instanceID),
		Size:           nifcloud.Int32(int32(capacity)),
		Description:    nifcloud.String(volumeName),
	}
	resp, err := c.computing.CreateVolume(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("could not create NIFCLOUD additional storage: %v", err)
	}

	volumeID := nifcloud.ToString(resp.VolumeId)
	if len(volumeID) == 0 {
		return nil, fmt.Errorf("volume ID was not returned by CreateVolume")
	}

	createdZone := nifcloud.ToString(resp.AvailabilityZone)
	if len(zone) == 0 {
		return nil, fmt.Errorf("availability zone was not returned by CreateVolume")
	}

	createdSize := nifcloud.ToInt32(resp.Size)
	if createdSize == 0 {
		return nil, fmt.Errorf("disk size was not returned by CreateVolume")
	}

	if err := c.waitForVolume(ctx, volumeID, "in-use"); err != nil {
		return nil, fmt.Errorf("failed to get an in-use volume: %v", err)
	}

	detachVolumeInput := &computing.DetachVolumeInput{
		Agreement:  nifcloud.Bool(true),
		InstanceId: nifcloud.String(instanceID),
		VolumeId:   nifcloud.String(volumeID),
	}
	if _, err = c.computing.DetachVolume(ctx, detachVolumeInput); err != nil {
		return nil, fmt.Errorf("could not detach additional storage %q from %q: %v", volumeID, instanceID, err)
	}

	if err := c.waitForVolume(ctx, volumeID, "available"); err != nil {
		return nil, fmt.Errorf("failed to get an available volume: %v", err)
	}

	return &Disk{
		CapacityGiB:        int64(createdSize),
		VolumeID:           volumeID,
		AvailabilityZone:   createdZone,
		AttachedInstanceID: "",
	}, nil
}

func (c *cloud) DeleteDisk(ctx context.Context, volumeID string) (bool, error) {
	input := &computing.DeleteVolumeInput{
		VolumeId: nifcloud.String(volumeID),
	}
	if _, err := c.computing.DeleteVolume(ctx, input); err != nil {
		if isAWSErrorVolumeNotFound(err) {
			return false, ErrNotFound
		}
		return false, fmt.Errorf("DeleteDisk could not delete volume: %v", err)
	}

	return true, nil
}

func (c *cloud) AttachDisk(ctx context.Context, volumeID, nodeID string) (string, error) {
	input := &computing.AttachVolumeInput{
		InstanceId: nifcloud.String(nodeID),
		VolumeId:   nifcloud.String(volumeID),
	}
	resp, err := c.computing.AttachVolume(ctx, input)
	if err != nil {
		if isAWSError(err, "Server.Inoperable.Volume.AlreadyAttached") {
			deviceName, err := c.getDeviceNameFromVolumeID(ctx, nodeID, volumeID)
			if err != nil {
				return "", fmt.Errorf("could not fetch the device name for already attached volume: %v", err)
			}
			return deviceName, nil
		}
		return "", fmt.Errorf("could not attach volume %q to node %q: %v", volumeID, nodeID, err)
	}
	klog.V(5).Infof("AttachVolume volume=%q instance=%q request returned %v", volumeID, nodeID, resp)

	// This is the only situation where we taint the device
	if err := c.WaitForAttachmentState(ctx, volumeID, "attached"); err != nil {
		return "", err
	}

	deviceName, err := c.getDeviceNameFromVolumeID(ctx, nodeID, volumeID)
	if err != nil {
		return "", fmt.Errorf("could not fetch the device name after attach volume: %v", err)
	}

	// TODO: Double check the attachment to be 100% sure we attached the correct volume at the correct mountpoint
	// It could happen otherwise that we see the volume attached from a previous/separate AttachVolume call,
	// which could theoretically be against a different device (or even instance).

	return deviceName, nil
}

func (c *cloud) DetachDisk(ctx context.Context, volumeID, nodeID string) error {
	input := &computing.DetachVolumeInput{
		InstanceId: nifcloud.String(nodeID),
		VolumeId:   nifcloud.String(volumeID),
		Agreement:  nifcloud.Bool(true),
	}
	if _, err := c.computing.DetachVolume(ctx, input); err != nil {
		if isAWSError(err, "Client.Inoperable.Volume.DetachedFromInstance") {
			return nil
		}
		return fmt.Errorf("could not detach volume %q from node %q: %v", volumeID, nodeID, err)
	}

	if err := c.WaitForAttachmentState(ctx, volumeID, "detached"); err != nil {
		return err
	}

	return nil
}

func (c *cloud) ResizeDisk(ctx context.Context, volumeID string, size int64) (int64, error) {
	volume, err := c.GetDiskByID(ctx, volumeID)
	if err != nil {
		return 0, err
	}

	currentSize := volume.CapacityGiB
	desiredSize := roundUpCapacity(util.BytesToGiB(size))
	if desiredSize-currentSize < 0 {
		return 0, fmt.Errorf(
			"could not resize %s's size to %d because it is smaller than current size %d",
			volumeID, desiredSize, currentSize,
		)
	} else if currentSize > desiredSize {
		// no need to resize.
		return currentSize, nil
	}

	// NIFCLOUD ExtendVolumeSize API can only grow in size by 100GiB
	// so, it loops until volume size reached the target size.
	for {
		input := &computing.ExtendVolumeSizeInput{
			NiftyReboot: types.NiftyRebootOfExtendVolumeSizeRequestFalse,
			VolumeId:    nifcloud.String(volumeID),
		}
		if _, err := c.computing.ExtendVolumeSize(ctx, input); err != nil {
			klog.Errorf("ExtendVolumeSize returns an error: %v", err)
			return 0, err
		}

		// wait for resized. after call ExtendVolumeSize API,
		// it returns resizing status in volumeSet.items[].attachmentSet.item[].status
		// (attaching: resizing -> attached: resize succeeded)
		if err := c.WaitForAttachmentState(ctx, volumeID, "attached"); err != nil {
			return 0, err
		}

		volume, err = c.GetDiskByID(ctx, volumeID)
		if err != nil {
			klog.Errorf("could not get the disk info from id: %v", err)
			return 0, err
		}
		klog.V(4).Infof("after extend volume: current=%dGiB, desired=%dGiB", volume.CapacityGiB, desiredSize)
		if volume.CapacityGiB >= desiredSize {
			break
		}
	}

	klog.V(4).Infof("resize succeeded! current volume size is %dGiB", volume.CapacityGiB)

	return volume.CapacityGiB, nil
}

func (c *cloud) ListDisks(ctx context.Context) ([]*Disk, error) {
	resp, err := c.computing.DescribeVolumes(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("clould not fetch the additional storages: %v", err)
	}

	disks := []*Disk{}
	for _, volume := range resp.VolumeSet {
		// Volume name was setted in volume description.
		// So use description to check this volume was created by Kubernetes CSI driver.
		if !strings.HasPrefix(nifcloud.ToString(volume.Description), "pvc-") {
			continue
		}

		volSize, err := strconv.Atoi(nifcloud.ToString(volume.Size))
		if err != nil {
			klog.Warningf("could not convert volume size %q. using 100GiB...: %v", nifcloud.ToString(volume.Size), err)
			volSize = 100
		}

		disks = append(disks, &Disk{
			VolumeID:           nifcloud.ToString(volume.VolumeId),
			CapacityGiB:        int64(volSize),
			AvailabilityZone:   nifcloud.ToString(volume.AvailabilityZone),
			AttachedInstanceID: getVolumeAttachedInstanceID(&volume),
		})
	}

	return disks, nil
}

func (c *cloud) WaitForAttachmentState(ctx context.Context, volumeID, state string) error {
	backoff := wait.Backoff{
		Duration: 3 * time.Second,
		Factor:   1.8,
		Steps:    13,
	}

	verifyVolumeFunc := func() (bool, error) {
		input := &computing.DescribeVolumesInput{
			VolumeId: []string{volumeID},
		}

		volume, err := c.getVolume(ctx, input)
		if err != nil {
			return false, err
		}

		if len(volume.AttachmentSet) == 0 {
			if state == "detached" {
				return true, nil
			}
		}

		for _, a := range volume.AttachmentSet {
			if a.Status == nil {
				klog.Warningf("Ignoring nil attachment state for volume %q: %v", volumeID, a)
				continue
			}
			if *a.Status == state {
				return true, nil
			}
		}
		return false, nil
	}

	return wait.ExponentialBackoff(backoff, verifyVolumeFunc)
}

func (c *cloud) GetDiskByName(ctx context.Context, name string, capacityBytes int64) (*Disk, error) {
	resp, err := c.computing.DescribeVolumes(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("could not list the volumes: %v", err)
	}

	var volume *types.VolumeSet
	for _, vol := range resp.VolumeSet {
		if nifcloud.ToString(vol.Description) == name {
			volume = &vol
		}
	}
	if volume == nil {
		return nil, ErrNotFound
	}

	volSizeGiB, err := strconv.Atoi(nifcloud.ToString(volume.Size))
	if err != nil {
		return nil, fmt.Errorf("could not convert volume size %q: %v", nifcloud.ToString(volume.Size), err)
	}

	if int64(volSizeGiB) != roundUpCapacity(util.BytesToGiB(capacityBytes)) {
		klog.Warningf(
			"disk size for %q is not same. request capacityBytes: %v != volume size: %v",
			name, roundUpCapacity(util.BytesToGiB(capacityBytes)), volSizeGiB,
		)
		return nil, ErrDiskExistsDiffSize
	}

	return &Disk{
		VolumeID:           nifcloud.ToString(volume.VolumeId),
		CapacityGiB:        int64(volSizeGiB),
		AvailabilityZone:   nifcloud.ToString(volume.AvailabilityZone),
		AttachedInstanceID: getVolumeAttachedInstanceID(volume),
	}, nil
}

func (c *cloud) GetDiskByID(ctx context.Context, volumeID string) (*Disk, error) {
	input := &computing.DescribeVolumesInput{
		VolumeId: []string{volumeID},
	}
	volume, err := c.getVolume(ctx, input)
	if err != nil {
		return nil, err
	}

	volSize, err := strconv.Atoi(nifcloud.ToString(volume.Size))
	if err != nil {
		return nil, fmt.Errorf("could not convert volume size %q: %v", nifcloud.ToString(volume.Size), err)
	}

	return &Disk{
		VolumeID:           nifcloud.ToString(volume.VolumeId),
		CapacityGiB:        int64(volSize),
		AvailabilityZone:   nifcloud.ToString(volume.AvailabilityZone),
		AttachedInstanceID: getVolumeAttachedInstanceID(volume),
	}, nil
}

func (c *cloud) IsExistInstance(ctx context.Context, nodeID string) bool {
	instance, err := c.getInstance(ctx, nodeID)
	if err != nil || instance == nil {
		return false
	}
	return true
}

func (c *cloud) GetInstanceByName(ctx context.Context, name string) (*Instance, error) {
	res, err := c.getInstance(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("could not found instance %q: %v", name, err)
	}

	return &Instance{
		InstanceID:       name,
		AvailabilityZone: nifcloud.ToString(res.Placement.AvailabilityZone),
	}, nil
}

// waitForVolume waits for volume to be in the "in-use" state.
func (c *cloud) waitForVolume(ctx context.Context, volumeID, status string) error {
	var (
		checkInterval = 3 * time.Second
		// This timeout can be "ovewritten" if the value returned by ctx.Deadline()
		// comes sooner. That value comes from the external provisioner controller.
		checkTimeout = 1 * time.Minute
	)

	input := &computing.DescribeVolumesInput{
		VolumeId: []string{volumeID},
	}

	err := wait.Poll(checkInterval, checkTimeout, func() (done bool, err error) {
		vol, err := c.getVolume(ctx, input)
		if err != nil {
			return true, err
		}
		if vol.Status != nil {
			return *vol.Status == status, nil
		}
		return false, nil
	})

	return err
}

func (c *cloud) listInstancesByZone(ctx context.Context, zone string) ([]Instance, error) {
	instances, err := c.listInstances(ctx)
	if err != nil {
		return nil, err
	}
	if len(instances) == 0 {
		return nil, fmt.Errorf("there are no instances in %s region", c.region)
	}

	result := []Instance{}
	for _, instance := range instances {
		if instance.AvailabilityZone == zone {
			result = append(result, instance)
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("there are no instances in %s zone", zone)
	}

	return result, nil
}

func (c *cloud) listInstances(ctx context.Context) ([]Instance, error) {
	resp, err := c.computing.DescribeInstances(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("error listing NIFCLOUD instances: %v", err)
	}

	instances := []Instance{}
	for _, reservation := range resp.ReservationSet {
		instance := reservation.InstancesSet[0]
		instances = append(instances, Instance{
			InstanceID:       nifcloud.ToString(instance.InstanceId),
			AvailabilityZone: nifcloud.ToString(instance.Placement.AvailabilityZone),
		})
	}

	return instances, nil
}

func (c *cloud) getInstance(ctx context.Context, nodeID string) (*types.InstancesSet, error) {
	input := &computing.DescribeInstancesInput{
		InstanceId: []string{nodeID},
	}
	resp, err := c.computing.DescribeInstances(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("error listing NIFCLOUD instances: %v", err)
	}

	instances := []types.InstancesSet{}
	for _, reservation := range resp.ReservationSet {
		instances = append(instances, reservation.InstancesSet...)
	}

	if l := len(instances); l > 1 {
		return nil, fmt.Errorf("found %d instances with ID %q", l, nodeID)
	} else if l < 1 {
		return nil, ErrNotFound
	}

	// DescribeInstances API does not return the deviceName in blockDeviceMapping.
	// deviceName can get from DescribeInstanceAttribute API.
	// So call DescribeInstanceAttribute API and set blockDeviceMapping to instance info.
	instance := &instances[0]
	if err := c.setBlockDeviceMapping(ctx, instance); err != nil {
		return nil, fmt.Errorf("error setting block device mapping: %v", err)
	}

	return instance, nil
}

func (c *cloud) setBlockDeviceMapping(ctx context.Context, instance *types.InstancesSet) error {
	input := &computing.DescribeInstanceAttributeInput{
		InstanceId: instance.InstanceId,
		Attribute:  types.AttributeOfDescribeInstanceAttributeRequestBlockDeviceMapping,
	}
	resp, err := c.computing.DescribeInstanceAttribute(ctx, input)
	if err != nil {
		return fmt.Errorf("error getting block device mapping: %v", err)
	}

	blockDeviceInfo := resp.BlockDeviceMapping
	blockDeviceMapping := make([]types.BlockDeviceMapping, len(blockDeviceInfo))
	for i, device := range blockDeviceInfo {
		ebs := &types.Ebs{}
		if device.Ebs != nil {
			ebs.AttachTime = device.Ebs.AttachTime
			ebs.DeleteOnTermination = device.Ebs.DeleteOnTermination
			ebs.Status = device.Ebs.Status
			ebs.VolumeId = device.Ebs.VolumeId
		}
		blockDeviceMapping[i] = types.BlockDeviceMapping{
			DeviceName: device.DeviceName,
			Ebs:        ebs,
		}
	}

	instance.BlockDeviceMapping = blockDeviceMapping

	return nil
}

func (c *cloud) getVolume(ctx context.Context, input *computing.DescribeVolumesInput) (*types.VolumeSet, error) {
	response, err := c.computing.DescribeVolumes(ctx, input)
	if err != nil {
		return nil, err
	}

	volumes := response.VolumeSet
	if l := len(volumes); l > 1 {
		return nil, ErrMultiDisks
	} else if l < 1 {
		return nil, ErrNotFound
	}

	return &volumes[0], nil
}

func (c *cloud) getDeviceNameFromVolumeID(ctx context.Context, instanceID, volumeID string) (string, error) {
	input := &computing.DescribeInstanceAttributeInput{
		InstanceId: nifcloud.String(instanceID),
		Attribute:  types.AttributeOfDescribeInstanceAttributeRequestBlockDeviceMapping,
	}
	resp, err := c.computing.DescribeInstanceAttribute(ctx, input)
	if err != nil {
		return "", fmt.Errorf("error getting block device mapping: %v", err)
	}

	for _, blockDevice := range resp.BlockDeviceMapping {
		if nifcloud.ToString(blockDevice.Ebs.VolumeId) == volumeID {
			return nifcloud.ToString(blockDevice.DeviceName), nil
		}
	}

	return "", fmt.Errorf("could not find device name for volume %q attached in %q", volumeID, instanceID)
}

// isAWSError returns a boolean indicating whether the error is AWS-related
// and has the given code. More information on AWS error codes at:
// NOTICE: nifcloud-sdk-go-v2 uses the aws-sdk-go-v2 error type
func isAWSError(err error, code string) bool {
	var awsErr smithy.APIError
	if errors.As(err, &awsErr) && awsErr.ErrorCode() == code {
		return true
	}
	return false
}

// isAWSErrorVolumeNotFound returns a boolean indicating whether the
// given error is an AWS InvalidVolume.NotFound error. This error is
// reported when the specified volume doesn't exist.
func isAWSErrorVolumeNotFound(err error) bool {
	return isAWSError(err, "Client.InvalidParameterNotFound.Volume")
}

func roundUpCapacity(capacityGiB int64) int64 {
	// NIFCLOUD additional storage unit
	// 100, 200, 300, ... 1000
	const unit = 100

	if capacityGiB%unit == 0 {
		return capacityGiB
	}
	return (capacityGiB/unit + 1) * unit
}

func getVolumeAttachedInstanceID(volume *types.VolumeSet) string {
	var attachedInstanceID string
	if len(volume.AttachmentSet) == 1 {
		attachedInstanceID = nifcloud.ToString(volume.AttachmentSet[0].InstanceId)
	} else {
		attachedInstanceID = ""
	}
	return attachedInstanceID
}
