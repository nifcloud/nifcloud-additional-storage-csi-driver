package driver

import (
	"context"
	"testing"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
)

func TestDriver_GetPluginInfo(t *testing.T) {
	d := &Driver{}

	ctx := context.Background()
	req := &csi.GetPluginInfoRequest{}

	want := &csi.GetPluginInfoResponse{
		Name:          DriverName,
		VendorVersion: driverVersion,
	}

	got, err := d.GetPluginInfo(ctx, req)

	if assert.NoError(t, err) {
		assert.Equal(t, want, got)
	}
}

func TestDriver_GetPluginCapabilities(t *testing.T) {
	d := &Driver{}

	ctx := context.Background()
	req := &csi.GetPluginCapabilitiesRequest{}

	want := &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS,
					},
				},
			},
		},
	}

	got, err := d.GetPluginCapabilities(ctx, req)

	if assert.NoError(t, err) {
		assert.Equal(t, want, got)
	}
}

func TestDriver_Probe(t *testing.T) {
	d := &Driver{}

	ctx := context.Background()
	req := &csi.ProbeRequest{}

	want := &csi.ProbeResponse{}

	got, err := d.Probe(ctx, req)

	if assert.NoError(t, err) {
		assert.Equal(t, want, got)
	}
}
