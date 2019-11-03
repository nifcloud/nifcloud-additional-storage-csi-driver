package cloud

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/alice02/nifcloud-sdk-go-v2/nifcloud"
	"github.com/alice02/nifcloud-sdk-go-v2/service/computing"
	dm "github.com/aokumasan/nifcloud-additional-storage-csi-driver/pkg/cloud/devicemanager"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/awserr"
	"github.com/kubernetes-sigs/aws-ebs-csi-driver/pkg/util"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
)

const (
	// VolumeTypeStandard represents a general purpose volume.
	VolumeTypeStandard = "standard"
	// VolumeTypeHighSpeedA represents a high speed volume.
	VolumeTypeHighSpeedA = "high-speed-a"
	// VolumeTypeHighSpeedB represents a high speed volume.
	VolumeTypeHighSpeedB = "high-speed-b"
	// VolumeTypeFlash represents a flash volume.
	VolumeTypeFlash = "flash"
)

var (
	// VolumeTypeMapping converts the volume identifier from volume type.
	// More info: https://pfs.nifcloud.com/api/rest/CreateVolume.htm
	VolumeTypeMapping = map[string]string{
		VolumeTypeStandard:   "2",
		VolumeTypeHighSpeedA: "3",
		VolumeTypeHighSpeedB: "4",
		VolumeTypeFlash:      "5",
	}
)

const (
	// DefaultVolumeSize represents the default volume size.
	DefaultVolumeSize int64 = 100 * util.GiB
	// DefaultVolumeType specifies which storage to use for newly created volumes.
	DefaultVolumeType = VolumeTypeHighSpeedA
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
	VolumeID         string
	CapacityGiB      int64
	AvailabilityZone string
}

// DiskOptions represents parameters to create an NIFCLOUD additional storage
type DiskOptions struct {
	CapacityBytes int64
	VolumeType    string
	InstanceID    string // temporary instance id to create the volume
}

// Cloud is interface for cloud api manipulator
type Cloud interface {
	CreateDisk(ctx context.Context, volumeName string, diskOptions *DiskOptions) (disk *Disk, err error)
	DeleteDisk(ctx context.Context, volumeID string) (success bool, err error)
	AttachDisk(ctx context.Context, volumeID string, nodeID string) (devicePath string, err error)
	DetachDisk(ctx context.Context, volumeID string, nodeID string) (err error)
	WaitForAttachmentState(ctx context.Context, volumeID, state string) error
	GetDiskByName(ctx context.Context, name string, capacityBytes int64) (disk *Disk, err error)
	GetDiskByID(ctx context.Context, volumeID string) (disk *Disk, err error)
	IsExistInstance(ctx context.Context, nodeID string) (success bool)
}

type cloud struct {
	region    string
	computing *computing.Client
	dm        dm.DeviceManager
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
		computing: computing.New(cfg),
		dm:        dm.NewDeviceManager(),
	}, nil
}

func (c *cloud) CreateDisk(ctx context.Context, volumeName string, diskOptions *DiskOptions) (*Disk, error) {
	var createType string
	switch diskOptions.VolumeType {
	case VolumeTypeStandard, VolumeTypeHighSpeedA, VolumeTypeHighSpeedB, VolumeTypeFlash:
		createType = VolumeTypeMapping[diskOptions.VolumeType]
	case "":
		createType = VolumeTypeMapping[DefaultVolumeType]
	default:
		return nil, fmt.Errorf("invalid NIFCLOUD VolumeType %q", diskOptions.VolumeType)
	}

	instanceID := diskOptions.InstanceID
	if instanceID == "" {
		return nil, errors.New("InstanceID is required")
	}

	capacity := roundUpCapacity(util.BytesToGiB(diskOptions.CapacityBytes))

	req := c.computing.CreateVolumeRequest(&computing.CreateVolumeInput{
		AccountingType: nifcloud.String("2"), // TODO: set accounting type from diskoptions
		DiskType:       nifcloud.String(createType),
		InstanceId:     nifcloud.String(instanceID),
		Size:           aws.Int64(capacity),
		Description:    aws.String(volumeName),
	})

	resp, err := req.Send(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not create NIFCLOUD additional storage: %v", err)
	}

	volumeID := aws.StringValue(resp.VolumeId)
	if len(volumeID) == 0 {
		return nil, fmt.Errorf("volume ID was not returned by CreateVolume")
	}

	zone := aws.StringValue(resp.AvailabilityZone)
	if len(zone) == 0 {
		return nil, fmt.Errorf("availability zone was not returned by CreateVolume")
	}

	createdSize, err := strconv.Atoi(aws.StringValue(resp.Size))
	if err != nil {
		return nil, fmt.Errorf("cannot convert disk size %q", aws.StringValue(resp.Size))
	}
	if createdSize == 0 {
		return nil, fmt.Errorf("disk size was not returned by CreateVolume")
	}

	if err := c.waitForVolume(ctx, volumeID, "in-use"); err != nil {
		return nil, fmt.Errorf("failed to get an in-use volume: %v", err)
	}

	detachVolumeRequest := c.computing.DetachVolumeRequest(&computing.DetachVolumeInput{
		Agreement:  aws.Bool(true),
		InstanceId: nifcloud.String(instanceID),
		VolumeId:   nifcloud.String(volumeID),
	})
	_, err = detachVolumeRequest.Send(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not detach additional storage %q from %q: %v", volumeID, instanceID, err)
	}

	if err := c.waitForVolume(ctx, volumeID, "available"); err != nil {
		return nil, fmt.Errorf("failed to get an available volume: %v", err)
	}

	return &Disk{
		CapacityGiB:      int64(createdSize),
		VolumeID:         volumeID,
		AvailabilityZone: zone,
	}, nil
}

func (c *cloud) DeleteDisk(ctx context.Context, volumeID string) (bool, error) {
	req := c.computing.DeleteVolumeRequest(&computing.DeleteVolumeInput{VolumeId: nifcloud.String(volumeID)})
	if _, err := req.Send(ctx); err != nil {
		if isAWSErrorVolumeNotFound(err) {
			return false, ErrNotFound
		}
		return false, fmt.Errorf("DeleteDisk could not delete volume: %v", err)
	}
	return true, nil
}

func (c *cloud) AttachDisk(ctx context.Context, volumeID, nodeID string) (string, error) {
	instance, err := c.getInstance(ctx, nodeID)
	if err != nil {
		return "", err
	}

	device, err := c.dm.NewDevice(instance, volumeID)
	if err != nil {
		return "", err
	}
	defer device.Release(false)

	if !device.IsAlreadyAssigned {
		resp, err := c.computing.AttachVolumeRequest(
			&computing.AttachVolumeInput{
				InstanceId: nifcloud.String(nodeID),
				VolumeId:   nifcloud.String(volumeID),
			},
		).Send(ctx)
		if err != nil {
			if awsErr, ok := err.(awserr.Error); ok {
				if awsErr.Code() == "Server.Inoperable.Volume.AlreadyAttached" {
					return "", ErrAlreadyExists
				}
			}
			return "", fmt.Errorf("could not attach volume %q to node %q: %v", volumeID, nodeID, err)
		}
		klog.V(5).Infof("AttachVolume volume=%q instance=%q request returned %v", volumeID, nodeID, resp)
	}

	// This is the only situation where we taint the device
	if err := c.WaitForAttachmentState(ctx, volumeID, "attached"); err != nil {
		device.Taint()
		return "", err
	}

	deviceName, err := c.getDeviceNameFromVolumeID(ctx, nodeID, volumeID)
	if err != nil {
		return "", fmt.Errorf("could not fetch the device name after attach volume: %v", err)
	}

	if device.Path == "" {
		device.Path = deviceName
	}

	if device.Path != deviceName {
		klog.Warningf("device path does not match: device.Path: %v, deviceName: %v", device.Path, deviceName)
		device.Path = deviceName
	}

	// TODO: Double check the attachment to be 100% sure we attached the correct volume at the correct mountpoint
	// It could happen otherwise that we see the volume attached from a previous/separate AttachVolume call,
	// which could theoretically be against a different device (or even instance).

	return device.Path, nil
}

func (c *cloud) DetachDisk(ctx context.Context, volumeID, nodeID string) error {
	instance, err := c.getInstance(ctx, nodeID)
	if err != nil {
		return err
	}

	// TODO: check if attached
	device, err := c.dm.GetDevice(instance, volumeID)
	if err != nil {
		return err
	}
	defer device.Release(true)

	if !device.IsAlreadyAssigned {
		klog.Warningf("DetachDisk called on non-attached volume: %s", volumeID)
	}

	_, err = c.computing.DetachVolumeRequest(&computing.DetachVolumeInput{
		InstanceId: nifcloud.String(nodeID),
		VolumeId:   nifcloud.String(volumeID),
		Agreement:  aws.Bool(true),
	}).Send(ctx)
	if err != nil {
		return fmt.Errorf("could not detach volume %q from node %q: %v", volumeID, nodeID, err)
	}

	if err := c.WaitForAttachmentState(ctx, volumeID, "detached"); err != nil {
		return err
	}

	return nil
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
	resp, err := c.computing.DescribeVolumesRequest(nil).Send(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not list the volumes: %v", err)
	}

	var volume *computing.VolumeSetItem
	for _, vol := range resp.VolumeSet {
		if *vol.Description == name {
			volume = &vol
		}
	}
	if volume == nil {
		return nil, ErrNotFound
	}

	volSizeGiB, err := strconv.Atoi(aws.StringValue(volume.Size))
	if err != nil {
		return nil, fmt.Errorf("could not convert volume size %q: %v", aws.StringValue(volume.Size), err)
	}

	if int64(volSizeGiB) != roundUpCapacity(util.BytesToGiB(capacityBytes)) {
		klog.Warningf(
			"disk size for %q is not same. request capacityBytes: %v != volume size: %v",
			name, roundUpCapacity(util.BytesToGiB(capacityBytes)), volSizeGiB,
		)
		return nil, ErrDiskExistsDiffSize
	}

	return &Disk{
		VolumeID:         aws.StringValue(volume.VolumeId),
		CapacityGiB:      int64(volSizeGiB),
		AvailabilityZone: aws.StringValue(volume.AvailabilityZone),
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

	volSize, err := strconv.Atoi(aws.StringValue(volume.Size))
	if err != nil {
		return nil, fmt.Errorf("could not convert volume size %q: %v", aws.StringValue(volume.Size), err)
	}

	return &Disk{
		VolumeID:         aws.StringValue(volume.VolumeId),
		CapacityGiB:      int64(volSize),
		AvailabilityZone: aws.StringValue(volume.AvailabilityZone),
	}, nil
}

func (c *cloud) IsExistInstance(ctx context.Context, nodeID string) bool {
	instance, err := c.getInstance(ctx, nodeID)
	if err != nil || instance == nil {
		return false
	}
	return true
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

func (c *cloud) getInstance(ctx context.Context, nodeID string) (*computing.InstancesSetItem, error) {
	request := c.computing.DescribeInstancesRequest(&computing.DescribeInstancesInput{
		InstanceId: []string{nodeID},
	})
	response, err := request.Send(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing NIFCLOUD instances: %v", err)
	}

	instances := []computing.InstancesSetItem{}
	for _, reservation := range response.ReservationSet {
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

func (c *cloud) setBlockDeviceMapping(ctx context.Context, instance *computing.InstancesSetItem) error {
	request := c.computing.DescribeInstanceAttributeRequest(
		&computing.DescribeInstanceAttributeInput{
			InstanceId: instance.InstanceId,
			Attribute:  nifcloud.String("blockDeviceMapping"),
		},
	)
	response, err := request.Send(ctx)
	if err != nil {
		return fmt.Errorf("error getting block device mapping: %v", err)
	}

	instance.BlockDeviceMapping = response.DescribeInstanceAttributeOutput.BlockDeviceMapping

	return nil
}

func (c *cloud) getVolume(ctx context.Context, input *computing.DescribeVolumesInput) (*computing.VolumeSetItem, error) {
	response, err := c.computing.DescribeVolumesRequest(input).Send(ctx)
	if err != nil {
		return nil, err
	}

	volumes := response.DescribeVolumesOutput.VolumeSet
	if l := len(volumes); l > 1 {
		return nil, ErrMultiDisks
	} else if l < 1 {
		return nil, ErrNotFound
	}

	return &volumes[0], nil
}

func (c *cloud) getDeviceNameFromVolumeID(ctx context.Context, instanceID, volumeID string) (string, error) {
	request := c.computing.DescribeInstanceAttributeRequest(
		&computing.DescribeInstanceAttributeInput{
			InstanceId: nifcloud.String(instanceID),
			Attribute:  nifcloud.String("blockDeviceMapping"),
		},
	)
	response, err := request.Send(ctx)
	if err != nil {
		return "", fmt.Errorf("error getting block device mapping: %v", err)
	}

	for _, blockDevice := range response.BlockDeviceMapping {
		if aws.StringValue(blockDevice.Ebs.VolumeId) == volumeID {
			return aws.StringValue(blockDevice.DeviceName), nil
		}
	}

	return "", fmt.Errorf("could not find device name for volume %q attached in %q", volumeID, instanceID)
}

// isAWSError returns a boolean indicating whether the error is AWS-related
// and has the given code. More information on AWS error codes at:
// NOTICE: nifcloud-sdk-go-v2 uses the aws-sdk-go-v2 error type
func isAWSError(err error, code string) bool {
	if awsError, ok := err.(awserr.Error); ok {
		if awsError.Code() == code {
			return true
		}
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
		return util.RoundUpGiB(capacityGiB)
	}
	return (util.RoundUpGiB(capacityGiB)/unit + 1) * unit
}
