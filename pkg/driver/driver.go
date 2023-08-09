package driver

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/aokumasan/nifcloud-additional-storage-csi-driver/pkg/util"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
)

const (
	// DriverName is name for this CSI
	DriverName = "additional-storage.csi.nifcloud.com"
	// TopologyKey is key
	TopologyKey = "topology." + DriverName + "/zone"
)

// Driver is CSI driver object
type Driver struct {
	controllerService
	nodeService

	srv     *grpc.Server
	options *DriverOptions
}

// DriverOptions is option for CSI driver.
type DriverOptions struct {
	endpoint string
}

// NewDriver creates the new CSI driver
func NewDriver(options ...func(*DriverOptions)) (*Driver, error) {
	klog.Infof("Driver: %v Version: %v", DriverName, driverVersion)

	instanceID, err := getInstanceID()
	if err != nil {
		panic(err)
	}

	driverOptions := DriverOptions{
		endpoint: DefaultCSIEndpoint,
	}
	for _, option := range options {
		option(&driverOptions)
	}

	driver := Driver{
		controllerService: newControllerService(instanceID),
		nodeService:       newNodeService(instanceID),
		options:           &driverOptions,
	}

	return &driver, nil
}

// Run runs the gRPC server
func (d *Driver) Run() error {
	scheme, addr, err := util.ParseEndpoint(d.options.endpoint)
	if err != nil {
		return err
	}

	listener, err := net.Listen(scheme, addr)
	if err != nil {
		return err
	}

	logErr := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			klog.Errorf("GRPC error: %v", err)
		}
		return resp, err
	}
	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(logErr),
	}
	d.srv = grpc.NewServer(opts...)

	csi.RegisterIdentityServer(d.srv, d)
	csi.RegisterControllerServer(d.srv, d)
	csi.RegisterNodeServer(d.srv, d)

	klog.Infof("Listening for connections on address: %#v", listener.Addr())

	return d.srv.Serve(listener)
}

// Stop stops the server
func (d *Driver) Stop() {
	klog.Infof("Stopping server")
	d.srv.Stop()
}

// WithEndpoint sets the endpoint
func WithEndpoint(endpoint string) func(*DriverOptions) {
	return func(o *DriverOptions) {
		o.endpoint = endpoint
	}
}

func getInstanceID() (string, error) {
	instanceID := os.Getenv("NODE_NAME")
	if instanceID == "" {
		return "", fmt.Errorf("the environment variable 'NODE_NAME' must not be empty")
	}

	return instanceID, nil
}
