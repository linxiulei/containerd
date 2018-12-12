package devicemapper

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
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

func TestBindRootfs(t *testing.T) {
	f := &FakeExec{
		Std:      []string{""},
		Executes: make([][]string, 1),
	}

	lm, err := NewLvmManager("", f)
	if err != nil {
		t.Fatal("NewLvmManager fail")
	}

	var dirs []string
	for _, i := range []string{"containerd_test_src", "containerd_test_dest"} {
		dir, err := ioutil.TempDir("/tmp", i)
		if err != nil {
			t.Fatalf("Create temp dir: %s fail", i)
		}

		dirs = append(dirs, dir)
		defer os.RemoveAll(dir)
	}

	err = lm.BindRootfs(dirs[0], dirs[1])
	if err != nil {
		t.Fatal("BindRootfs fail")
	}

	var args []string
	args = []string{"mount", "--bind", filepath.Join(dirs[0], "rootfs"), dirs[1]}
	if !reflect.DeepEqual(f.Executes[0], args) {
		t.Fatalf("Cmd %v should be executed, but %v", args, f.Executes[0])
	}

	fi, err := os.Lstat(filepath.Join(dirs[0], "rootfs"))
	if err != nil {
		t.Fatal("Lstat fail")
	}

	if !fi.Mode().IsDir() {
		t.Fatal("rootfs was not created as dir")
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

func TestTrimDeviceSubdir(t *testing.T) {
	testsuites := []struct {
		input  string
		output string
	}{
		{
			input:  "/dev/mapper/vg0-1[/rootfs]",
			output: "/dev/mapper/vg0-1",
		},
		{
			input:  "/dev/mapper/vg0-1",
			output: "/dev/mapper/vg0-1",
		},
	}

	for _, test := range testsuites {
		output := trimDeviceSubdir(test.input)
		if output != test.output {
			t.Fatalf("trimDeviceSubdir should be %s, receive %s", test.output, output)
		}
	}
}
