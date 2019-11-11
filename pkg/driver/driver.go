package driver

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-sigs/aws-ebs-csi-driver/pkg/util"
	"github.com/mattn/go-shellwords"
	"google.golang.org/grpc"
	"k8s.io/klog"
)

const (
	// DriverName is name for this CSI
	DriverName = "additional-storage.csi.nifcloud.com"
	// TopologyKey is key
	TopologyKey = "topology." + DriverName + "/zone"
)

var execCommand = exec.Command

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

	instanceID, err := getInstanceIDFromGuestInfo()
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

func getInstanceIDFromGuestInfo() (string, error) {
	const cmd = "vmtoolsd --cmd 'info-get guestinfo.hostname'"
	args, err := shellwords.Parse(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to parse command %q: %v", cmd, err)
	}
	out, err := execCommand(args[0], args[1:]...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("could not get instance id from vmtoolsd: %v (%v)", string(out), err)
	}

	return strings.TrimSpace(string(out)), nil
}
