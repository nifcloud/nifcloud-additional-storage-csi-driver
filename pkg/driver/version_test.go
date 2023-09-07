package driver_test

import (
	"fmt"
	"runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nifcloud/nifcloud-additional-storage-csi-driver/pkg/driver"
)

var _ = Describe("version", func() {
	Describe("GetVersion", func() {

		BeforeEach(func() {
			driver.SetupVersion()
		})

		AfterEach(func() {
			driver.ResetVersion()
		})

		Context("valid", func() {
			It("should return Version", func() {

				expected := driver.VersionInfo{
					DriverVersion: driver.TestDriverVersion,
					GitCommit:     driver.TestGitCommit,
					BuildDate:     driver.TestBuildDate,
					GoVersion:     runtime.Version(),
					Compiler:      runtime.Compiler,
					Platform:      fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
				}

				actual := driver.GetVersion()
				Expect(actual).Should(Equal(expected))
			})
		})
	})

	Describe("GetVersionJSON", func() {
		const versionJsonFormat = `{
  "driverVersion": "%s",
  "gitCommit": "%s",
  "buildDate": "%s",
  "goVersion": "%s",
  "compiler": "%s",
  "platform": "%s"
}`

		BeforeEach(func() {
			driver.SetupVersion()
		})

		AfterEach(func() {
			driver.ResetVersion()
		})

		Context("valid", func() {
			It("should return VersionJSON", func() {

				expected := fmt.Sprintf(
					versionJsonFormat,
					driver.TestDriverVersion,
					driver.TestGitCommit,
					driver.TestBuildDate,
					runtime.Version(),
					runtime.Compiler,
					fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
				)

				actual, err := driver.GetVersionJSON()
				Expect(err).ShouldNot(HaveOccurred())
				Expect(actual).Should(Equal(expected))
			})
		})
	})
})
