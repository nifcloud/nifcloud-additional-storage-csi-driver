package driver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/kubernetes/pkg/util/mount"
)

func Test_newNodeMounter(t *testing.T) {
	got := newNodeMounter()
	if assert.NotNil(t, got) {
		assert.IsType(t, &NodeMounter{}, got)
	}
}

func TestNodeMounter_GetDeviceName(t *testing.T) {
	fakeMounter := &mount.FakeMounter{
		MountPoints: []mount.MountPoint{
			{
				Device: "/dev/disk/by-path/testdisk",
				Path:   "/mnt/test",
			},
		},
	}
	type args struct {
		mountPath string
	}
	tests := []struct {
		name         string
		args         args
		wantDevice   string
		wantRefCount int
		wantErr      bool
	}{
		{
			name: "return device successfully",
			args: args{
				mountPath: "/mnt/test",
			},
			wantDevice:   "/dev/disk/by-path/testdisk",
			wantRefCount: 1,
		},
		{
			name: "return no device if no device was mounted on path",
			args: args{
				mountPath: "/mnt/notmounted",
			},
			wantDevice:   "",
			wantRefCount: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &NodeMounter{
				mount.SafeFormatAndMount{
					Interface: fakeMounter,
					Exec:      mount.NewOsExec(),
				},
			}
			got, got1, err := m.GetDeviceName(tt.args.mountPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("NodeMounter.GetDeviceName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.wantDevice {
				t.Errorf("NodeMounter.GetDeviceName() got = %v, want %v", got, tt.wantDevice)
			}
			if got1 != tt.wantRefCount {
				t.Errorf("NodeMounter.GetDeviceName() got1 = %v, want %v", got1, tt.wantRefCount)
			}
		})
	}
}
