package driver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"code.k9.ms/vpsie-csi/util"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
)

const (
	DefaultDriverName = "vpsie.csi"
)

var (
	version string
)

type Config struct {
	DriverName  string
	EndPoint    string
	NodeID      string
	ShowVersion bool
	Url         string
	DataCenter  string
	Token       string
}

type Driver struct {
	srv    *grpc.Server
	config Config

	readyMu sync.Mutex // protects ready
	ready   bool
}

func NewDriver(cfg *Config) (*Driver, error) {
	if cfg.DriverName == "" {
		return nil, errors.New("driver name is empty")
	}
	if cfg.NodeID == "" {
		return nil, errors.New("node id is empty")
	}
	if cfg.Token == "" {
		return nil, errors.New("token is empty")
	}
	if cfg.EndPoint == "" {
		return nil, errors.New("end point is empty")
	}

	return &Driver{config: *cfg}, nil
}

func GetVersion() string {
	return version
}

func (d *Driver) Run(ctx context.Context) error {
	scheme, addr, err := util.ParseEndpoint(d.config.EndPoint)
	if err != nil {
		return err
	}

	grpListener, err := net.Listen(scheme, addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

	logErr := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			klog.ErrorS(err, "Grpc error")
		}
		return resp, err
	}

	d.srv = grpc.NewServer(grpc.UnaryInterceptor(logErr))
	csi.RegisterIdentityServer(d.srv, d)
	// csi.RegisterControllerServer(d.srv, d)
	// csi.RegisterNodeServer(d.srv, d)

	klog.V(4).InfoS("Listening for connections", "address", grpListener.Addr())
	return d.srv.Serve(grpListener)
}

func (d *Driver) Stop() {
	d.srv.Stop()
}
