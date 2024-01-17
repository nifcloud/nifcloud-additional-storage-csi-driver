package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/nifcloud/nifcloud-additional-storage-csi-driver/pkg/driver"
	"k8s.io/klog/v2"
)

func main() {
	var (
		version             bool
		endpoint            string
		nifcloudSdkDebugLog bool
	)

	flag.BoolVar(&version, "version", false, "Print the version and exit.")
	flag.StringVar(&endpoint, "endpoint", driver.DefaultCSIEndpoint, "CSI Endpoint")
	flag.BoolVar(&nifcloudSdkDebugLog, "nifcloud-sdk-debug-log", false, "To enable the nifcloud debug log level (default to false).")

	klog.InitFlags(nil)
	flag.Parse()

	if version {
		info, err := driver.GetVersionJSON()
		if err != nil {
			klog.ErrorS(err, "Failed to GetVersionJSON")
			klog.FlushAndExit(klog.ExitFlushTimeout, 1)
		}
		fmt.Println(info)
		os.Exit(0)
	}

	drv, err := driver.NewDriver(
		driver.WithEndpoint(endpoint),
		driver.WithNifcloudSdkDebugLog(nifcloudSdkDebugLog),
	)
	if err != nil {
		klog.ErrorS(err, "Failed to NewDriver")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	if err := drv.Run(); err != nil {
		klog.ErrorS(err, "Failed to driver Run")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
}
