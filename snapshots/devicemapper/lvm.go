package devicemapper

import (
	"fmt"
	"os/exec"
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
		"-s", fmt.Sprintf("%s/%s", lm.volumeGroup, volumeName),
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
		"lvremove", "-y", fmt.Sprintf("%s/%s", lm.volumeGroup, volumeName),
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
		fmt.Sprintf("%s/%s", lm.volumeGroup, volumeName),
	}
	std, err := lm.exe.ExecCombinedOutput(args)
	if err != nil {
		return errors.Wrap(err, std)
	}
	return nil
}

func (lm *LvmManager) Mount(volumeName, path string) error {
	args := []string{"mount", fmt.Sprintf("/dev/%s/%s", lm.volumeGroup, volumeName), path}
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
	gv := strings.Split(std, "-")
	if len(gv) != 2 {
		return "", fmt.Errorf("gv is less than 2: %s", gv)
	}
	return gv[1], nil
}

func (e *exe) ExecCombinedOutput(args []string) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	std, err := cmd.CombinedOutput()
	return string(std), err
}
