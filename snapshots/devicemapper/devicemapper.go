package devicemapper

import (
	"context"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/platforms"
	"github.com/containerd/containerd/plugin"
	"github.com/containerd/containerd/snapshots"
	"github.com/containerd/containerd/snapshots/storage"

	"github.com/containerd/continuity/fs"
	"github.com/pkg/errors"
)

func init() {
	plugin.Register(&plugin.Registration{
		Type: plugin.SnapshotPlugin,
		ID:   "devicemapper",
		InitFn: func(ic *plugin.InitContext) (interface{}, error) {
			ic.Meta.Platforms = append(ic.Meta.Platforms, platforms.DefaultSpec())
			return NewSnapshotter(ic.Root)
		},
	})
}

type snapshotter struct {
	root string
	ms   *storage.MetaStore
	lm   *LvmManager
}

const (
	RootParentVolume = "RootVolume"
	VolumeGroup      = "vg0"
)

// NewSnapshotter returns a Snapshotter which copies layers on the underlying
// file system. A metadata file is stored under the root.
func NewSnapshotter(root string) (snapshots.Snapshotter, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	ms, err := storage.NewMetaStore(filepath.Join(root, "metadata.db"))
	if err != nil {
		return nil, err
	}

	if err := os.Mkdir(filepath.Join(root, "snapshots"), 0700); err != nil && !os.IsExist(err) {
		return nil, err
	}

	lm, err := NewLvmManager(VolumeGroup, nil)
	if err != nil {
		return nil, err
	}

	return &snapshotter{
		root: root,
		ms:   ms,
		lm:   lm,
	}, nil
}

// Stat returns the info for an active or committed snapshot by name or
// key.
//
// Should be used for parent resolution, existence checks and to discern
// the kind of snapshot.
func (o *snapshotter) Stat(ctx context.Context, key string) (snapshots.Info, error) {
	ctx, t, err := o.ms.TransactionContext(ctx, false)
	if err != nil {
		return snapshots.Info{}, err
	}
	defer t.Rollback()
	_, info, _, err := storage.GetInfo(ctx, key)
	if err != nil {
		return snapshots.Info{}, err
	}

	return info, nil
}

func (o *snapshotter) Update(ctx context.Context, info snapshots.Info, fieldpaths ...string) (snapshots.Info, error) {
	ctx, t, err := o.ms.TransactionContext(ctx, true)
	if err != nil {
		return snapshots.Info{}, err
	}

	info, err = storage.UpdateInfo(ctx, info, fieldpaths...)
	if err != nil {
		t.Rollback()
		return snapshots.Info{}, err
	}

	if err := t.Commit(); err != nil {
		return snapshots.Info{}, err
	}

	return info, nil
}

func (o *snapshotter) Usage(ctx context.Context, key string) (snapshots.Usage, error) {
	ctx, t, err := o.ms.TransactionContext(ctx, false)
	if err != nil {
		return snapshots.Usage{}, err
	}
	defer t.Rollback()

	id, info, usage, err := storage.GetInfo(ctx, key)
	if err != nil {
		return snapshots.Usage{}, err
	}

	if info.Kind == snapshots.KindActive {
		du, err := fs.DiskUsage(ctx, o.getSnapshotDir(id))
		if err != nil {
			return snapshots.Usage{}, err
		}
		usage = snapshots.Usage(du)
	}

	return usage, nil
}

func (o *snapshotter) Prepare(ctx context.Context, key, parent string, opts ...snapshots.Opt) ([]mount.Mount, error) {
	return o.createSnapshot(ctx, snapshots.KindActive, key, parent, opts)
}

func (o *snapshotter) View(ctx context.Context, key, parent string, opts ...snapshots.Opt) ([]mount.Mount, error) {
	return o.createSnapshot(ctx, snapshots.KindView, key, parent, opts)
}

// Mounts returns the mounts for the transaction identified by key. Can be
// called on an read-write or readonly transaction.
//
// This can be used to recover mounts after calling View or Prepare.
func (o *snapshotter) Mounts(ctx context.Context, key string) ([]mount.Mount, error) {
	ctx, t, err := o.ms.TransactionContext(ctx, false)
	if err != nil {
		return nil, err
	}
	s, err := storage.GetSnapshot(ctx, key)
	t.Rollback()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get snapshot mount")
	}
	return o.mounts(s), nil
}

func (o *snapshotter) Commit(ctx context.Context, name, key string, opts ...snapshots.Opt) error {
	ctx, t, err := o.ms.TransactionContext(ctx, true)
	if err != nil {
		return err
	}

	id, _, _, err := storage.GetInfo(ctx, key)
	if err != nil {
		return err
	}

	usage, err := fs.DiskUsage(ctx, o.getSnapshotDir(id))
	if err != nil {
		return err
	}

	if _, err := storage.CommitActive(ctx, key, name, snapshots.Usage(usage), opts...); err != nil {
		if rerr := t.Rollback(); rerr != nil {
			log.G(ctx).WithError(rerr).Warn("failed to rollback transaction")
		}
		return errors.Wrap(err, "failed to commit snapshot")
	}
	return t.Commit()
}

// Remove abandons the transaction identified by key. All resources
// associated with the key will be removed.
func (o *snapshotter) Remove(ctx context.Context, key string) (err error) {
	ctx, t, err := o.ms.TransactionContext(ctx, true)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil && t != nil {
			if rerr := t.Rollback(); rerr != nil {
				log.G(ctx).WithError(rerr).Warn("failed to rollback transaction")
			}
		}
	}()

	id, _, err := storage.Remove(ctx, key)
	if err != nil {
		return errors.Wrap(err, "failed to remove")
	}

	path := o.getSnapshotDir(id)
	volume, err := o.lm.GetVolumeByPath(path)
	if err != nil {
		return errors.Wrapf(err, "failed to get volume by path: %s", path)
	}

	err = o.lm.Unmount(path)
	if err != nil {
		return errors.Wrap(err, "failed to unmount")
	}

	err = o.lm.RemoveVolume(volume)
	if err != nil {
		return errors.Wrapf(err, "failed to remove volume: %s", volume)
	}

	renamed := filepath.Join(o.root, "snapshots", "rm-"+id)
	if err := os.Rename(path, renamed); err != nil {
		if !os.IsNotExist(err) {
			return errors.Wrap(err, "failed to rename")
		}
		renamed = ""
	}

	err = t.Commit()
	t = nil
	if err != nil {
		if renamed != "" {
			if err1 := os.Rename(renamed, path); err1 != nil {
				// May cause inconsistent data on disk
				log.G(ctx).WithError(err1).WithField("path", renamed).Errorf("failed to rename after failed commit")
			}
		}
		return errors.Wrap(err, "failed to commit")
	}
	if renamed != "" {
		if err := os.RemoveAll(renamed); err != nil {
			// Must be cleaned up, any "rm-*" could be removed if no active transactions
			log.G(ctx).WithError(err).WithField("path", renamed).Warnf("failed to remove root filesystem")
		}
	}

	return nil
}

// Walk the committed snapshots.
func (o *snapshotter) Walk(ctx context.Context, fn func(context.Context, snapshots.Info) error) error {
	ctx, t, err := o.ms.TransactionContext(ctx, false)
	if err != nil {
		return err
	}
	defer t.Rollback()
	return storage.WalkInfo(ctx, fn)
}

func (o *snapshotter) createSnapshot(ctx context.Context, kind snapshots.Kind, key, parent string, opts []snapshots.Opt) ([]mount.Mount, error) {
	var (
		err  error
		path string
	)

	ctx, t, err := o.ms.TransactionContext(ctx, true)
	if err != nil {
		return nil, err
	}

	s, err := storage.CreateSnapshot(ctx, kind, key, parent, opts...)
	if err != nil {
		if rerr := t.Rollback(); rerr != nil {
			log.G(ctx).WithError(rerr).Warn("failed to rollback transaction")
		}
		return nil, errors.Wrap(err, "failed to create snapshot")
	}

	path = o.getSnapshotDir(s.ID)
	if err = os.MkdirAll(path, 0700); err != nil {
		return nil, errors.Wrapf(err, "failed create path")
	}

	var parentVolume string
	if parent == "" || s.ParentIDs[0] == "" {
		parentVolume = RootParentVolume
	} else {
		parentPath := o.getSnapshotDir(s.ParentIDs[0])
		parentVolume, err = o.lm.GetVolumeByPath(parentPath)
		if err != nil {
			return nil, errors.Wrap(err, "failed to find parent volume")
		}
	}

	err = o.lm.CreateSnapshot(parentVolume, s.ID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create snapshot")
	}

	err = o.lm.Mount(s.ID, path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to mount volume")
	}

	if err := t.Commit(); err != nil {
		return nil, errors.Wrap(err, "commit failed")
	}

	return o.mounts(s), nil
}

func (o *snapshotter) getSnapshotDir(id string) string {
	return filepath.Join(o.root, "snapshots", id)
}

func (o *snapshotter) mounts(s storage.Snapshot) []mount.Mount {
	var (
		roFlag string
		source string
	)

	if s.Kind == snapshots.KindView {
		roFlag = "ro"
	} else {
		roFlag = "rw"
	}

	if len(s.ParentIDs) == 0 || s.Kind == snapshots.KindActive {
		source = o.getSnapshotDir(s.ID)
	} else {
		source = o.getSnapshotDir(s.ParentIDs[0])
	}

	return []mount.Mount{
		{
			Source: source,
			Type:   "bind",
			Options: []string{
				roFlag,
				"rbind",
			},
		},
	}
}

// Close closes the snapshotter
func (o *snapshotter) Close() error {
	return o.ms.Close()
}
