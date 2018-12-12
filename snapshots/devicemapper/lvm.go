package devicemapper

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
)

type CmdExec interface {
	ExecCombinedOutput(args []string) (string, error)
}

type exe struct{}

func NewCmdExec() *exe {
	return &exe{}
}

type LvmManager struct {
	volumeGroup string
	exe         CmdExec
}

func NewLvmManager(volumeGroup string, exe CmdExec) (*LvmManager, error) {
	if exe == nil {
		return &LvmManager{
			volumeGroup: volumeGroup,
			exe:         NewCmdExec(),
		}, nil
	}

	return &LvmManager{
		volumeGroup: volumeGroup,
		exe:         exe,
	}, nil
}

func (lm *LvmManager) CreateSnapshot(volumeName, snapshotName string) error {
	args := []string{
		"lvcreate", "--type", "thin",
		"-s", lm.getFullVolumeName(volumeName),
		"-n", snapshotName}

	std, err := lm.exe.ExecCombinedOutput(args)
	if err != nil {
		return errors.Wrap(err, std)
	}

	err = lm.activateVolume(snapshotName)
	if err != nil {
		return err
	}
	return nil
}

func (lm *LvmManager) RemoveVolume(volumeName string) error {
	args := []string{
		"lvremove", "-y", lm.getFullVolumeName(volumeName),
	}
	std, err := lm.exe.ExecCombinedOutput(args)
	if err != nil {
		return errors.Wrap(err, std)
	}
	return nil
}

func (lm *LvmManager) activateVolume(volumeName string) error {
	args := []string{
		"lvchange", "-ay", "-Ky",
		lm.getFullVolumeName(volumeName),
	}
	std, err := lm.exe.ExecCombinedOutput(args)
	if err != nil {
		return errors.Wrap(err, std)
	}
	return nil
}

func (lm *LvmManager) BindRootfs(srcDir, destDir string) error {
	rootFsDir := filepath.Join(srcDir, "rootfs")
	if err := os.Mkdir(rootFsDir, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	args := []string{"mount", "--bind", rootFsDir, destDir}
	std, err := lm.exe.ExecCombinedOutput(args)
	if err != nil {
		return errors.Wrap(err, std)
	}
	return nil
}

func (lm *LvmManager) Mount(volumeName, path string) error {
	args := []string{
		"mount", "/dev/" + lm.getFullVolumeName(volumeName), path,
	}
	std, err := lm.exe.ExecCombinedOutput(args)
	if err != nil {
		return errors.Wrap(err, std)
	}
	return nil
}

func (lm *LvmManager) Unmount(path string) error {
	args := []string{"umount", path}
	std, err := lm.exe.ExecCombinedOutput(args)
	if err != nil {
		return errors.Wrap(err, std)
	}
	return nil
}

func (lm *LvmManager) GetVolumeByPath(path string) (string, error) {
	args := []string{"findmnt", "-n", "-o", "SOURCE", path}
	std, err := lm.exe.ExecCombinedOutput(args)
	if err != nil {
		return "", err
	}
	std = strings.TrimSpace(std)
	std = trimDeviceSubdir(std)
	gv := strings.Split(std, "-")
	if len(gv) != 2 {
		return "", fmt.Errorf("gv is less than 2: %s", gv)
	}
	return gv[1], nil
}

func (lm *LvmManager) getFullVolumeName(volumeName string) string {
	return fmt.Sprintf("%s/%s", lm.volumeGroup, volumeName)
}

func (e *exe) ExecCombinedOutput(args []string) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	std, err := cmd.CombinedOutput()
	return string(std), err
}

// trim /dev/mapper/vg0-1[/rootfs]
func trimDeviceSubdir(devSrc string) string {
	gv := strings.Split(devSrc, "[")
	return gv[0]
}
