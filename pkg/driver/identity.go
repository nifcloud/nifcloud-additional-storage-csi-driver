package driver

import (
	"context"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"k8s.io/klog"
)

func (d *Driver) GetPluginInfo(ctx context.Context, req *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	klog.V(6).Infof("GetPluginInfo: called with args %+v", *req)
	resp := &csi.GetPluginInfoResponse{
		Name:          DriverName,
		VendorVersion: driverVersion,
	}

	return resp, nil
}

func (d *Driver) GetPluginCapabilities(ctx context.Context, req *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	klog.V(6).Infof("GetPluginCapabilities: called with args %+v", *req)
	resp := &csi.GetPluginCapabilitiesResponse{
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

	return resp, nil
}

func (d *Driver) Probe(ctx context.Context, req *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	klog.V(6).Infof("Probe: called with args %+v", *req)
	return &csi.ProbeResponse{}, nil
}
