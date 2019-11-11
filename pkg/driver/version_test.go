package driver

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func setup() {
	driverVersion = "testversion"
	gitCommit = "testcommit"
	buildDate = "testdate"
}

func reset() {
	driverVersion = ""
	gitCommit = ""
	buildDate = ""
}

func TestGetVersion(t *testing.T) {
	setup()
	defer reset()

	want := VersionInfo{
		DriverVersion: "testversion",
		GitCommit:     "testcommit",
		BuildDate:     "testdate",
		GoVersion:     runtime.Version(),
		Compiler:      runtime.Compiler,
		Platform:      fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}

	got := GetVersion()

	assert.Equal(t, want, got)
}

func TestGetVersionJSON(t *testing.T) {
	setup()
	defer reset()

	want := fmt.Sprintf(`{
  "driverVersion": "testversion",
  "gitCommit": "testcommit",
  "buildDate": "testdate",
  "goVersion": "%s",
  "compiler": "%s",
  "platform": "%s"
}`, runtime.Version(), runtime.Compiler, fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH))

	got, err := GetVersionJSON()

	if assert.NoError(t, err) {
		assert.Equal(t, want, got)
	}
}
