package driver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"code.k9.ms/vpsie-csi/pkg/govpsie"
	"code.k9.ms/vpsie-csi/util"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/oauth2"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
)

const (
	DefaultDriverName = "vpsie.csi.vpsie.com"
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
	StorageTag  string               


	ClientID     string
	ClientSecret string
}

type Driver struct {
	srv    *grpc.Server
	config Config
	// publishInfoVolumeName is used to pass the volume name from
	// `ControllerPublishVolume` to `NodeStageVolume or `NodePublishVolume`
	publishInfoVolumeName string
	mounter               Mounter

	storage   govpsie.StorageService
	account   govpsie.AccountService
	vpsie govpsie.VpsieService
	snapshots govpsie.SnapshotService

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

	ts := tknSource{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
	}
	client := govpsie.NewClient(oauth2.NewClient(context.Background(), &ts))

	client.SetUserAgent("vpsie-csi-driver/" + version)

	return &Driver{
		config:                *cfg,
		storage:               client.Storage,
		account:               client.Account,
		snapshots:             client.Snapshot,
		vpsie: client.Vpsie,
		publishInfoVolumeName: cfg.DriverName + "/volume-name",
		mounter:               newMounter(),
	}, nil
}

func GetVersion() string {
	return version
}

func (d *Driver) Run(ctx context.Context) error {
	scheme, addr, err := util.ParseEndpoint(d.config.EndPoint)
	if err != nil {
		return err
	}

	klog.V(4).InfoS("Listening for connections", "scheme", scheme, "address", addr)

	grpcListener, err := net.Listen(scheme, addr)
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
	csi.RegisterControllerServer(d.srv, d)
	csi.RegisterNodeServer(d.srv, d)

	var eg errgroup.Group

	
	d.ready = true

	eg.Go(func() error {
		go func() {
			<-ctx.Done()
			klog.V(4).InfoS("server stopped")
			d.readyMu.Lock()
			d.ready = false
			d.readyMu.Unlock()
			d.srv.GracefulStop()
		}()
		klog.V(4).InfoS("Listening for connections", "address", grpcListener.Addr())
		return d.srv.Serve(grpcListener)
	})

	return eg.Wait()
}

func (d *Driver) Stop() {
	d.srv.Stop()
}
