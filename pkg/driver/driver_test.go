package driver

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"code.k9.ms/vpsie-csi/pkg/govpsie"
	"github.com/kubernetes-csi/csi-test/v4/pkg/sanity"
	"golang.org/x/sync/errgroup"
	"k8s.io/mount-utils"
)

func TestSanity(t *testing.T) {
	// socket := "/tmp/csi.sock"
	// endpoint := "unix://" + socket
	// if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
	// 	t.Fatalf("failed to remove unix domain socket file %s, error: %s", socket, err)
	// }

	dir, err := os.MkdirTemp("", "sanity-ebs-csi")
	if err != nil {
		t.Fatalf("error creating directory %v", err)
	}
	defer os.RemoveAll(dir)

	targetPath := filepath.Join(dir, "mount")
	stagingPath := filepath.Join(dir, "staging")
	endpoint := "unix://" + filepath.Join(dir, "csi.sock")


	fm := fakeMounter{
		mounted: map[string]string{},
	}

	storags := make(map[string]*govpsie.Storage)
	vpsies := make(map[string]*govpsie.VmData)

	driver := &Driver{
		config: Config{
			EndPoint: endpoint,
			DriverName: "fake",
			NodeID: "vcIdentifier",
			DataCenter: "nyc",
			StorageTag: "",
			ClientID: "",
			ClientSecret: "",
		},
		mounter: &fm,
		storage: &mockStorage{
			storages: storags,
		},
		account: nil,
		vpsie: &mockVpsie{
			vpsies: vpsies,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	var eg errgroup.Group
	eg.Go(func() error {
		return driver.Run(ctx)
	})

	
	config := sanity.TestConfig{
		Address:          endpoint,
		CheckPath: fm.checkMountPath,
		TargetPath:       targetPath,
		StagingPath:      stagingPath,
		CreateTargetDir:  createDir,
		CreateStagingDir: createDir,
	}

	if err := os.RemoveAll(config.TargetPath); err != nil {
		t.Fatalf("failed to delete target path %s: %s", config.TargetPath, err)
	}
	if err := os.RemoveAll(config.StagingPath); err != nil {
		t.Fatalf("failed to delete staging path %s: %s", config.StagingPath, err)
	}
	// Now call the test suite
	sanity.Test(t, config)

	cancel()
	if err := eg.Wait(); err != nil {
		t.Errorf("driver run failed: %s", err)
	}
	
}

func createDir(targetPath string) (string, error) {
	if err := os.MkdirAll(targetPath, 0300); err != nil {
		if os.IsNotExist(err) {
			return "", err
		}
	}
	return targetPath, nil
}

type fakeMounter struct {
	mounted map[string]string
}

func (f *fakeMounter) Format(source string, fsType string) error {
	return nil
}

func (f *fakeMounter) Mount(source string, target string, fsType string, options ...string) error {
	f.mounted[target] = source
	return nil
}

func (f *fakeMounter) Unmount(target string) error {
	delete(f.mounted, target)
	return nil
}

func (f *fakeMounter) GetDeviceName(_ mount.Interface, mountPath string) (string, error) {
	if _, ok := f.mounted[mountPath]; ok {
		return "/mnt/sda1", nil
	}

	return "", nil
}

func (f *fakeMounter) IsAttached(source string) error {
	return nil
}

func (f *fakeMounter) IsFormatted(source string) (bool, error) {
	return true, nil
}
func (f *fakeMounter) IsMounted(target string) (bool, error) {
	_, ok := f.mounted[target]
	return ok, nil
}

func (f *fakeMounter) checkMountPath(path string) (sanity.PathKind, error) {
	isMounted, err := f.IsMounted(path)
	if err != nil {
		return "", err
	}
	if isMounted {
		return sanity.PathIsDir, nil
	}
	return sanity.PathIsNotFound, nil
}

func (f *fakeMounter) GetStatistics(volumePath string) (volumeStatistics, error) {
	return volumeStatistics{
		availableBytes: 3 * giB,
		totalBytes:     10 * giB,
		usedBytes:      7 * giB,

		availableInodes: 3000,
		totalInodes:     10000,
		usedInodes:      7000,
	}, nil
}

func (f *fakeMounter) IsBlockDevice(volumePath string) (bool, error) {
	return false, nil
}

type mockStorage struct {
	storages map[string]*govpsie.Storage
}

func(m *mockStorage) List(ctx context.Context, options *govpsie.ListOptions) ([]govpsie.Storage, error) {
	var storages []govpsie.Storage

	for _, vol := range m.storages {
		storages = append(storages, *vol)
	}

	return storages, nil
}

func(m *mockStorage) Delete(ctx context.Context, storageIdentifier string) error {
	delete(m.storages, storageIdentifier)
	return nil
}
func(m *mockStorage) AttachToVPSie(ctx context.Context, storageIdentifier, vmIdentifier string) error {
	m.storages[storageIdentifier].VmIdentifier = vmIdentifier
	return nil
}
func(m *mockStorage) DetachToVPSie(ctx context.Context, storageIdentifier, vmIdentifier string) error {
	m.storages[storageIdentifier].VmIdentifier = vmIdentifier
	return nil
}
func(m *mockStorage) CreateContainer(ctx context.Context, dcIdentifier string) error {
	return nil
}
func(m *mockStorage) ListAll(ctx context.Context, options *govpsie.ListOptions) ([]govpsie.Storage, error) {
	return nil, nil
}
func(m *mockStorage) Update(ctx context.Context, updateReq *govpsie.StorageUpdateRequest) error {
	volume := m.storages[updateReq.StorageIdentifier]
	volume.Size = updateReq.Size
	return nil
}
func(m *mockStorage) Create(ctx context.Context, createReq *govpsie.StorageCreateRequest, vmIdentifier string) error {
	return nil
} 
func(m *mockStorage) CreateStorage(ctx context.Context, createReq *govpsie.StorageCreateRequest) error {
	id := randString(12)
	vol := &govpsie.Storage{

	}

	m.storages[id] = vol
	return nil
}

type mockVpsie struct {
	vpsies map[string]*govpsie.VmData
}

func(m *mockVpsie) ListVpsie(context.Context, *govpsie.ListOptions, string) ([]govpsie.VmData, error) {
	return nil, nil
}
func(m *mockVpsie)  GetVpsieByIdentifier(ctx context.Context, vcIdentifier string) (*govpsie.VmData, error) {
	vpsie, ok :=  m.vpsies[vcIdentifier]
	if !ok {
		return nil, errors.New("vpsie not found")
	}

	return vpsie, nil
}

func(m *mockVpsie)  GetVpsieStatusByIdentifier(context.Context, string) (*govpsie.Status, error) {
	return nil, nil
}
func(m *mockVpsie)  GetVpsieConsole(ctx context.Context, identifierId string) (*govpsie.VpsieConsole, error) {
	return nil, nil
}
func(m *mockVpsie)  CreateVpsie(context.Context, *govpsie.CreateVpsieRequest) error {
	return nil
}
func(m *mockVpsie)  DeleteVpsie(ctx context.Context, identifierId string) error {
	return nil
}
func(m *mockVpsie)  StartVpsie(ctx context.Context, identifierId string) error {
	return nil
}
func(m *mockVpsie)  StopVpsie(ctx context.Context, identifierId string) error {
	return nil
}
func(m *mockVpsie)  RestartVpsie(ctx context.Context, identifierId string) error {
	return nil
}
func(m *mockVpsie) ChangePassword(ctx context.Context, identifierId string, newPassword string) error {
	return nil
}
func(m *mockVpsie) ChangeHostName(ctx context.Context, identifierId string, newHostname string) error {
	return nil
}
func(m *mockVpsie) AddVPC(ctx context.Context, request *govpsie.VpcRequest) error {
	return nil
}
func(m *mockVpsie)MoveVPC(ctx context.Context, request *govpsie.VpcRequest) error {
	return nil
}
func(m *mockVpsie) AddTags(ctx context.Context, identifierId string, tags []string) error {
	return nil
}
func(m *mockVpsie) ResizeVpsie(ctx context.Context, identifierId, cpu, ram string) error {
	return nil
}
func(m *mockVpsie) AddSsh(ctx context.Context, identifierId, sshKeyIdentifier string) error {
	return nil
}
func(m *mockVpsie) AddScript(ctx context.Context, identifierId, scriptIdentifier string) error {
	return nil
}
func(m *mockVpsie) ToggleLock(ctx context.Context, identifierId string) error {
	return nil
}
func(m *mockVpsie) DoMultiActions(ctx context.Context, vmsIdentifiers []string, actionType, sshKeyIdentifier string) error {
	return nil
}
func(m *mockVpsie) EnableIpv6(ctx context.Context, identifierId string) error {
	return nil
}
func(m *mockVpsie) AddFip(ctx context.Context, identifierId, dcIdentifier string) error {
	return nil
}


func randString(n int) string {
	const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}