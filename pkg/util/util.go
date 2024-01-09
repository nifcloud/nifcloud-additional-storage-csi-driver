// This code was forked from https://github.com/kubernetes-sigs/aws-ebs-csi-driver/blob/v1.21.0/pkg/util/util.go

package util

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	GiB = 1024 * 1024 * 1024
)

// RoundUpBytes rounds up the volume size in bytes upto multiplications of GiB
// in the unit of Bytes
func RoundUpBytes(volumeSizeBytes int64) (int64, error) {
	bytes, err := roundUpSize(volumeSizeBytes, GiB)
	if err != nil {
		return bytes, err
	}
	return bytes * GiB, nil
}

// RoundUpGiB rounds up the volume size in bytes upto multiplications of GiB
// in the unit of GiB
func RoundUpGiB(volumeSizeBytes int64) (int64, error) {
	return roundUpSize(volumeSizeBytes, GiB)
}

// BytesToGiB converts Bytes to GiB
func BytesToGiB(volumeSizeBytes int64) int64 {
	return volumeSizeBytes / GiB
}

// GiBToBytes converts GiB to Bytes
func GiBToBytes(volumeSizeGiB int64) int64 {
	return volumeSizeGiB * GiB
}

func ParseEndpoint(endpoint string) (string, string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", "", fmt.Errorf("could not parse endpoint: %w", err)
	}

	addr := filepath.Join(u.Host, filepath.FromSlash(u.Path))

	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "tcp":
	case "unix":
		addr = filepath.Join("/", addr)
		if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
			return "", "", fmt.Errorf("could not remove unix domain socket %q: %w", addr, err)
		}
	default:
		return "", "", fmt.Errorf("unsupported protocol: %s", scheme)
	}

	return scheme, addr, nil
}

func roundUpSize(volumeSizeBytes int64, allocationUnitBytes int64) (int64, error) {
	// commonly, round up for negative value is allowed, but unnecessary in this project
	if volumeSizeBytes < 0 {
		return 0, fmt.Errorf("volumeSizeBytes should 0 or more")
	} else if allocationUnitBytes <= 0 {
		return 0, fmt.Errorf("allocationUnitBytes should grater than 0")
	} else if (volumeSizeBytes + allocationUnitBytes - 1) < 0 {
		return 0, fmt.Errorf("roundUpSize value overflows int64")
	}
	return (volumeSizeBytes + allocationUnitBytes - 1) / allocationUnitBytes, nil
}
