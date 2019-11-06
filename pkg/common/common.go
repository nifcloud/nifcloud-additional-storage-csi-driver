package common

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/mattn/go-shellwords"
)

func GetInstanceID() (string, error) {
	const cmd = "vmtoolsd --cmd 'info-get guestinfo.hostname'"
	args, err := shellwords.Parse(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to parse command %q: %v", cmd, err)
	}
	out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("could not get instance id using vmtoolsd: %v (%v)", string(out), err)
	}

	return strings.TrimSpace(string(out)), nil
}
