package driver_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/nifcloud/nifcloud-additional-storage-csi-driver/pkg/driver"
)

var _ = Describe("identity", func() {

	Describe("GetPluginInfo", func() {
		BeforeEach(func() {
			driver.SetupVersion()
		})

		AfterEach(func() {
			driver.ResetVersion()
		})

		Context("valid", func() {
			It("should return GetPluginInfoResponse", func() {
				ctx := context.Background()
				req := &csi.GetPluginInfoRequest{}

				expected := &csi.GetPluginInfoResponse{
					Name:          driver.DriverName,
					VendorVersion: driver.TestDriverVersion,
				}

				d := &driver.Driver{}
				actual, err := d.GetPluginInfo(ctx, req)

				Expect(err).ShouldNot(HaveOccurred())
				Expect(actual).Should(Equal(expected))
			})
		})
	})

	Describe("GetPluginCapabilities", func() {
		Context("valid", func() {
			It("should return GetPluginCapabilitiesResponse", func() {
				ctx := context.Background()
				req := &csi.GetPluginCapabilitiesRequest{}

				expected := &csi.GetPluginCapabilitiesResponse{
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

				d := &driver.Driver{}
				actual, err := d.GetPluginCapabilities(ctx, req)

				Expect(err).ShouldNot(HaveOccurred())
				Expect(actual).Should(Equal(expected))
			})
		})
	})

	Describe("Probe", func() {
		Context("valid", func() {
			It("should return ProbeResponse", func() {
				ctx := context.Background()
				req := &csi.ProbeRequest{}

				expected := &csi.ProbeResponse{}

				d := &driver.Driver{}
				actual, err := d.Probe(ctx, req)

				Expect(err).ShouldNot(HaveOccurred())
				Expect(actual).Should(Equal(expected))
			})
		})
	})
})
