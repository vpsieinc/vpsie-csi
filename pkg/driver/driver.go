package driver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	"code.k9.ms/vpsie-csi/util"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/vpsie/govpsie"
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
	MaxRetries  int
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
	vpsie     govpsie.ServerService
	snapshots govpsie.SnapshotService

	readyMu sync.Mutex // protects ready
	ready   bool
}

func NewDriver(cfg *Config) (*Driver, error) {
	klog.Infof("Driver: %v version: %v", cfg.DriverName, version)
	printNodeInfo()

	if cfg.DriverName == "" {
		return nil, errors.New("driver name is empty")
	}
	klog.Infof("Driver name: %v", cfg.DriverName)
	if cfg.Token == "" {
		return nil, errors.New("token is empty")
	}
	klog.Info("Token is not empty")
	if cfg.EndPoint == "" {
		return nil, errors.New("end point is empty")
	}
	klog.Infof("End point: %v", cfg.EndPoint)

	// ts := oauth2.StaticTokenSource(&oauth2.Token{
	// 	AccessToken: cfg.Token,
	// })

	klog.Info("Token source created: ", cfg.Token)

	// client := govpsie.NewClient(oauth2.NewClient(context.Background(), ts))
	// disable ssl verification
	client := govpsie.NewClient(&http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	})

	klog.Info("Client created")

	client.SetUserAgent("vpsie-csi-driver/" + version)

	klog.Info("User agent set")

	if cfg.Url != "" {
		klog.Infof("Base url: %v", cfg.Url)
		client.SetBaseURL(cfg.Url)
	}

	klog.Info("Base url set")

	client.SetRequestHeaders(map[string]string{
		"Vpsie-Auth": cfg.Token,
	})

	klog.Info("Request headers set")

	hostName := os.Getenv("HOSTNAME")

	// list all vpsies and search for specific one by name hostName
	vms, err := client.Storage.ListVmsToAttach(context.Background())

	klog.Info("List vms to attach")
	if err != nil {
		klog.Error("Failed to list vms to attach: %v", err)
		return nil, err
	}

	klog.Info("Listed vms to attach %v", vms)

	var curentVm *govpsie.VmToAttach
	for _, vm := range vms {
		if strings.ToLower(vm.Hostname) == hostName {
			curentVm = &vm
			break
		}
	}

	klog.Info("Curent vm: ", curentVm)

	if curentVm == nil || curentVm.Hostname == "" {
		return nil, fmt.Errorf("vpsie with name %s not found", hostName)
	}

	cfg.NodeID = curentVm.Identifier
	cfg.DataCenter = curentVm.DcIdentifier

	klog.Info("datacenter: ", cfg.DataCenter)
	klog.Info("nodeID: ", cfg.NodeID)

	return &Driver{
		config:                *cfg,
		storage:               client.Storage,
		account:               client.Account,
		snapshots:             client.Snapshot,
		vpsie:                 client.Server,
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
