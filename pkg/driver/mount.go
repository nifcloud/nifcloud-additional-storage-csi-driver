package driver

import "k8s.io/kubernetes/pkg/util/mount"

// Mounter is an interface for mount operations
type Mounter interface {
	mount.Interface
	mount.Exec
	FormatAndMount(source string, target string, fstype string, options []string) error
	GetDeviceName(mountPath string) (string, int, error)
}

// NodeMounter mount the devices.
type NodeMounter struct {
	mount.SafeFormatAndMount
}

func newNodeMounter() Mounter {
	return &NodeMounter{
		mount.SafeFormatAndMount{
			Interface: mount.New(""),
			Exec:      mount.NewOsExec(),
		},
	}
}

// GetDeviceName returns the device name from path
func (m *NodeMounter) GetDeviceName(mountPath string) (string, int, error) {
	return mount.GetDeviceNameFromMount(m, mountPath)
}
