package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/nifcloud/nifcloud-additional-storage-csi-driver/pkg/driver"
	"k8s.io/component-base/featuregate"
	"k8s.io/component-base/logs"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/component-base/logs/json"
	"k8s.io/klog/v2"
)

func main() {
	var (
		version             bool
		endpoint            string
		nifcloudSdkDebugLog bool
		logToStderr         bool
	)

	flag.BoolVar(&version, "version", false, "Print the version and exit.")
	flag.StringVar(&endpoint, "endpoint", driver.DefaultCSIEndpoint, "CSI Endpoint")
	flag.BoolVar(&nifcloudSdkDebugLog, "nifcloud-sdk-debug-log", false, "To enable the nifcloud debug log level (default to false).")
	flag.BoolVar(&logToStderr, "logtostderr", false, "Output log to standard error. DEPRECATED: will be removed in a future release.")

	if err := logsapi.RegisterLogFormat(logsapi.JSONLogFormat, json.Factory{}, logsapi.LoggingBetaOptions); err != nil {
		klog.ErrorS(err, "Failed to register JSON log format")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	fg := featuregate.NewFeatureGate()
	if err := logsapi.AddFeatureGates(fg); err != nil {
		klog.ErrorS(err, "Failed to add feature gates")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	c := logsapi.NewLoggingConfiguration()
	logsapi.AddGoFlags(c, flag.CommandLine)
	logs.InitLogs()

	flag.Parse()

	if err := logsapi.ValidateAndApply(c, fg); err != nil {
		klog.ErrorS(err, "Failed to logsapi ValidateAndApply")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	if version {
		info, err := driver.GetVersionJSON()
		if err != nil {
			klog.ErrorS(err, "Failed to GetVersionJSON")
			klog.FlushAndExit(klog.ExitFlushTimeout, 1)
		}
		fmt.Println(info)
		os.Exit(0)
	}

	if logToStderr {
		klog.SetOutput(os.Stderr)
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
