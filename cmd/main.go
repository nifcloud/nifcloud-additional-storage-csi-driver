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
		version  bool
		endpoint string
	)

	flag.BoolVar(&version, "version", false, "Print the version and exit.")
	flag.StringVar(&endpoint, "endpoint", driver.DefaultCSIEndpoint, "CSI Endpoint")

	klog.InitFlags(nil)
	flag.Parse()

	if version {
		info, err := driver.GetVersionJSON()
		if err != nil {
			klog.Fatalln(err)
		}
		fmt.Println(info)
		os.Exit(0)
	}

	drv, err := driver.NewDriver(
		driver.WithEndpoint(endpoint),
	)
	if err != nil {
		klog.Fatalln(err)
	}
	if err := drv.Run(); err != nil {
		klog.Fatalln(err)
	}
}
