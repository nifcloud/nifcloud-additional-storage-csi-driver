package driver

const (
	TestDriverVersion = "testversion"
	TestGitCommit     = "testcommit"
	TestBuildDate     = "testdate"
)

func SetupVersion() {
	driverVersion = TestDriverVersion
	gitCommit = TestGitCommit
	buildDate = TestBuildDate
}

func ResetVersion() {
	driverVersion = ""
	gitCommit = ""
	buildDate = ""
}
