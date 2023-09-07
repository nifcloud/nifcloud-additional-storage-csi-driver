package driver

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/utils/exec"
	"k8s.io/utils/mount"
)

var _ = Describe("mount", func() {
	Describe("GetDeviceName", func() {
		var fakeMounter *mount.FakeMounter

		BeforeEach(func() {
			fakeMounter = &mount.FakeMounter{
				MountPoints: []mount.MountPoint{
					{
						Device: "/dev/disk/by-path/testdisk",
						Path:   "/mnt/test",
					},
				},
			}
		})

		Context("valid", func() {
			It("should return mounted device", func() {
				m := &NodeMounter{
					mount.SafeFormatAndMount{
						Interface: fakeMounter,
						Exec:      exec.New(),
					},
					exec.New(),
				}
				name, refCount, err := m.GetDeviceName("/mnt/test")
				Expect(err).ShouldNot(HaveOccurred())
				Expect(name).Should(Equal("/dev/disk/by-path/testdisk"))
				Expect(refCount).Should(Equal(1))
			})

			It("should return no device if no device was mounted on path", func() {
				m := &NodeMounter{
					mount.SafeFormatAndMount{
						Interface: fakeMounter,
						Exec:      exec.New(),
					},
					exec.New(),
				}
				name, refCount, err := m.GetDeviceName("/mnt/notmounted")
				Expect(err).ShouldNot(HaveOccurred())
				Expect(name).Should(Equal(""))
				Expect(refCount).Should(Equal(0))
			})
		})
	})
})
