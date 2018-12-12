package devicemapper

import (
	"fmt"
	"reflect"
	"testing"
)

type FakeExec struct {
	Std      []string
	Executes [][]string
	count    int
}

func (f *FakeExec) ExecCombinedOutput(args []string) (string, error) {
	f.Executes[f.count] = args
	f.count += 1
	return f.Std[f.count-1], nil
}

func TestCreateSnapshot(t *testing.T) {
	var (
		volumeGroup = "vg0"
		pvolume     = "volume1"
		snap        = "snap2"
	)

	f := &FakeExec{
		Std:      []string{"", "", ""},
		Executes: make([][]string, 3),
	}
	lm, err := NewLvmManager(volumeGroup, f)
	if err != nil {
		t.Fatal("NewLvmManager fail")
	}

	err = lm.CreateSnapshot(pvolume, snap)
	if err != nil {
		t.Fatal("CreateSnapshot fail")
	}

	var args []string
	args = []string{"lvcreate", "--type", "thin", "-s", volumeGroup + "/" + pvolume, "-n", snap}
	if !reflect.DeepEqual(f.Executes[0], args) {
		t.Fatalf("Cmd %v should be executed, but %v", args, f.Executes[0])
	}

	args = []string{"lvchange", "-ay", "-Ky", volumeGroup + "/" + snap}
	if !reflect.DeepEqual(f.Executes[1], args) {
		t.Fatalf("Cmd %v should be executed, but %v", args, f.Executes[1])
	}

	err = lm.RemoveVolume(snap)
	if err != nil {
		t.Fatal("RemoveVolume fail")
	}

	args = []string{"lvremove", "-y", volumeGroup + "/" + snap}
	if !reflect.DeepEqual(f.Executes[2], args) {
		t.Fatalf("Cmd %v should be executed, but %v", args, f.Executes[2])
	}
}

func TestMount(t *testing.T) {
	var (
		volumeGroup = "vg0"
		snap        = "snap2"
		mnt         = "/mnt"
	)

	f := &FakeExec{
		Std:      []string{"", ""},
		Executes: make([][]string, 2),
	}

	lm, err := NewLvmManager(volumeGroup, f)
	if err != nil {
		t.Fatal("NewLvmManager fail")
	}

	err = lm.Mount(snap, mnt)
	if err != nil {
		t.Fatal("Mount fail")
	}

	var args []string
	args = []string{"mount", fmt.Sprintf("/dev/%s/%s", volumeGroup, snap), mnt}
	if !reflect.DeepEqual(f.Executes[0], args) {
		t.Fatalf("Cmd %v should be executed, but %v", args, f.Executes[0])
	}

	err = lm.Unmount(mnt)
	if err != nil {
		t.Fatal("Unount fail")
	}
	args = []string{"umount", mnt}
	if !reflect.DeepEqual(f.Executes[1], args) {
		t.Fatalf("Cmd %v should be executed, but %v", args, f.Executes[1])
	}
}

func TestGetVolumeByPath(t *testing.T) {
	var (
		volumeGroup = "vg0"
		snap        = "snap2"
		mnt         = "/mnt"
	)

	f := &FakeExec{
		Std:      []string{fmt.Sprintf("%s-%s", volumeGroup, snap)},
		Executes: make([][]string, 1),
	}

	lm, err := NewLvmManager(volumeGroup, f)
	if err != nil {
		t.Fatal("NewLvmManager fail")
	}

	volume, err := lm.GetVolumeByPath(mnt)
	if volume != snap {
		t.Fatalf("GetVolumeByPath should be %s, receive %s", snap, volume)
	}
}
