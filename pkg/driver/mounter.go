package driver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"k8s.io/klog/v2"
	utilexec "k8s.io/utils/exec"

	"k8s.io/mount-utils"
)

const (
	blkidExitStatusNoIdentifiers = 2
)

type findmntResponse struct {
	FileSystems []fileSystem `json:"filesystems"`
}

type fileSystem struct {
	Target      string `json:"target"`
	Propagation string `json:"propagation"`
	FsType      string `json:"fstype"`
	Options     string `json:"options"`
}

type volumeStatistics struct {
	availableBytes, totalBytes, usedBytes    int64
	availableInodes, totalInodes, usedInodes int64
}

type mounter struct {
	kMounter *mount.SafeFormatAndMount
}

type Mounter interface {
	mount.Interface

	Format(source, fsType string) error
	// Mount(source, target, fsType string, options ...string) error
	Unmount(target string) error
	IsAttached(target string) (bool, error)
	IsMounted(target string) (bool, error)
	IsFormatted(source string) (bool, error)
	GetDeviceName(mounter mount.Interface, mountPath string) (string, error)
	GetStatistics(volumePath string) (volumeStatistics, error)
	IsBlockDevice(volumePath string) (bool, error)
}

func newMounter() *mounter {
	kMounter := &mount.SafeFormatAndMount{
		Interface: mount.New(""),
		Exec:      utilexec.New(),
	}

	return &mounter{
		kMounter: kMounter,
	}
}
func (m *mounter) Format(source, fsType string) error {
	mkfsCmd := fmt.Sprintf("mkfs.%s", fsType)

	_, err := exec.LookPath(mkfsCmd)
	if err != nil {
		if err == exec.ErrNotFound {
			return fmt.Errorf("%q executable not found in $PATH", mkfsCmd)
		}
		return err
	}

	mkfsArgs := []string{}

	if fsType == "" {
		return errors.New("fs type is not specified for formatting the volume")
	}

	if source == "" {
		return errors.New("source is not specified for formatting the volume")
	}

	mkfsArgs = append(mkfsArgs, source)
	if fsType == "ext4" || fsType == "ext3" {
		mkfsArgs = []string{"-F", source}
	}

	out, err := exec.Command(mkfsCmd, mkfsArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("formatting disk failed: %v cmd: '%s %s' output: %q",
			err, mkfsCmd, strings.Join(mkfsArgs, " "), string(out))
	}

	return nil
}

func (m *mounter) Mount(source, target, fsType string, opts ...string) error {
	mountCmd := "mount"
	mountArgs := []string{}

	if source == "" {
		return errors.New("source is not specified for mounting the volume")
	}

	if target == "" {
		return errors.New("target is not specified for mounting the volume")
	}

	// This is a raw block device mount. Create the mount point as a file
	// since bind mount device node requires it to be a file
	if fsType == "" {
		// create directory for target, os.Mkdirall is noop if directory exists
		err := os.MkdirAll(filepath.Dir(target), 0750)
		if err != nil {
			return fmt.Errorf("failed to create target directory for raw block bind mount: %v", err)
		}

		file, err := os.OpenFile(target, os.O_CREATE, 0660)
		if err != nil {
			return fmt.Errorf("failed to create target file for raw block bind mount: %v", err)
		}
		file.Close()
	} else {
		mountArgs = append(mountArgs, "-t", fsType)

		// create target, os.Mkdirall is noop if directory exists
		err := os.MkdirAll(target, 0750)
		if err != nil {
			return err
		}
	}

	if len(opts) > 0 {
		mountArgs = append(mountArgs, "-o", strings.Join(opts, ","))
	}

	mountArgs = append(mountArgs, source)
	mountArgs = append(mountArgs, target)

	klog.Infof("executing mount command cmd: %s, args: %s", mountCmd, mountArgs)

	out, err := exec.Command(mountCmd, mountArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mounting failed: %v cmd: '%s %s' output: %q",
			err, mountCmd, strings.Join(mountArgs, " "), string(out))
	}

	return nil
}

func (m *mounter) Unmount(target string) error {
	return mount.CleanupMountPoint(target, m.kMounter, true)
}

func (m *mounter) IsMounted(target string) (bool, error) {
	if target == "" {
		return false, errors.New("target is not specified for checking the mount")
	}

	findmntCmd := "findmnt"
	_, err := exec.LookPath(findmntCmd)
	if err != nil {
		if err == exec.ErrNotFound {
			return false, fmt.Errorf("%q executable not found in $PATH", findmntCmd)
		}
		return false, err
	}

	findmntArgs := []string{"-o", "TARGET,PROPAGATION,FSTYPE,OPTIONS", "-M", target, "-J"}

	klog.Infof("checking if target is mounted cmd: %s, args: %s", findmntCmd, findmntArgs)

	out, err := exec.Command(findmntCmd, findmntArgs...).CombinedOutput()
	if err != nil {
		// findmnt exits with non zero exit status if it couldn't find anything
		if strings.TrimSpace(string(out)) == "" {
			return false, nil
		}

		return false, fmt.Errorf("checking mounted failed: %v cmd: %q output: %q",
			err, findmntCmd, string(out))
	}

	// no response means there is no mount
	if string(out) == "" {
		return false, nil
	}

	var resp *findmntResponse
	err = json.Unmarshal(out, &resp)
	if err != nil {
		return false, fmt.Errorf("couldn't unmarshal data: %q: %s", string(out), err)
	}

	targetFound := false
	for _, fs := range resp.FileSystems {
		// check if the mount is propagated correctly. It should be set to shared.
		if fs.Propagation != "shared" {
			return true, fmt.Errorf("mount propagation for target %q is not enabled", target)
		}

		// the mountpoint should match as well
		if fs.Target == target {
			targetFound = true
		}
	}

	return targetFound, nil
}

func (m *mounter) IsFormatted(source string) (bool, error) {
	if source == "" {
		return false, errors.New("source is not specified")
	}

	blkidCmd := "blkid"
	_, err := exec.LookPath(blkidCmd)
	if err != nil {
		if err == exec.ErrNotFound {
			return false, fmt.Errorf("%q executable not found in $PATH", blkidCmd)
		}
		return false, err
	}

	blkidArgs := []string{source}

	exitCode := 0
	cmd := exec.Command(blkidCmd, blkidArgs...)
	err = cmd.Run()
	if err != nil {
		exitError, ok := err.(*exec.ExitError)
		if !ok {
			return false, fmt.Errorf("checking formatting failed: %v cmd: %q, args: %q", err, blkidCmd, blkidArgs)
		}
		ws := exitError.Sys().(syscall.WaitStatus)
		exitCode = ws.ExitStatus()
		if exitCode == blkidExitStatusNoIdentifiers {
			return false, nil
		}
		return false, fmt.Errorf("checking formatting failed: %v cmd: %q, args: %q", err, blkidCmd, blkidArgs)
	}

	return true, nil
}

func (m *mounter) GetDeviceName(mounter mount.Interface, mountPath string) (string, error) {
	devicePath, _, err := mount.GetDeviceNameFromMount(mounter, mountPath)
	return devicePath, err
}

func (m *mounter) GetStatistics(volumePath string) (volumeStatistics, error) {
	isBlock, err := m.IsBlockDevice(volumePath)
	if err != nil {
		return volumeStatistics{}, fmt.Errorf("failed to determine if volume %s is block device: %v", volumePath, err)
	}

	if isBlock {
		// See http://man7.org/linux/man-pages/man8/blockdev.8.html for details
		output, err := exec.Command("blockdev", "getsize64", volumePath).CombinedOutput()
		if err != nil {
			return volumeStatistics{}, fmt.Errorf("error when getting size of block volume at path %s: output: %s, err: %v", volumePath, string(output), err)
		}
		strOut := strings.TrimSpace(string(output))
		gotSizeBytes, err := strconv.ParseInt(strOut, 10, 64)
		if err != nil {
			return volumeStatistics{}, fmt.Errorf("failed to parse size %s into int", strOut)
		}

		return volumeStatistics{
			totalBytes: gotSizeBytes,
		}, nil
	}

	// var statfs unix.S_IFMT
	// // See http://man7.org/linux/man-pages/man2/statfs.2.html for details.
	// err = unix.Statfs(volumePath, &statfs)
	// if err != nil {
	// 	return volumeStatistics{}, err
	// }

	volStats := volumeStatistics{
		// availableBytes: int64(statfs.Bavail) * int64(statfs.Bsize),
		// totalBytes:     int64(statfs.Blocks) * int64(statfs.Bsize),
		// usedBytes:      (int64(statfs.Blocks) - int64(statfs.Bfree)) * int64(statfs.Bsize),

		// availableInodes: int64(statfs.Ffree),
		// totalInodes:     int64(statfs.Files),
		// usedInodes:      int64(statfs.Files) - int64(statfs.Ffree),
	}

	return volStats, nil
}

func (m *mounter) IsBlockDevice(devicePath string) (bool, error) {
	// var stat unix.Stat_t
	// err := unix.Stat(devicePath, &stat)
	// if err != nil {
	// 	return false, err
	// }

	// return (stat.Mode & unix.S_IFMT) == unix.S_IFBLK, nil

	return true, nil
}
