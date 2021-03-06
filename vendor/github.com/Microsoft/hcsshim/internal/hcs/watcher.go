package hcs

import (
	"time"

	"github.com/Microsoft/hcsshim/internal/logfields"
	"github.com/Microsoft/hcsshim/internal/timeout"
	"github.com/sirupsen/logrus"
)

// syscallWatcher is used as a very simple goroutine around calls into
// the platform. In some cases, we have seen HCS APIs not returning due to
// various bugs, and the goroutine making the syscall ends up not returning,
// prior to its async callback. By spinning up a syscallWatcher, it allows
// us to at least log a warning if a syscall doesn't complete in a reasonable
// amount of time.
//
// Usage is:
//
// completed := false
// go syscallWatcher(context, &completed)
// <syscall>
// completed = true
//
func syscallWatcher(context logrus.Fields, syscallCompleted *bool) {
	time.Sleep(timeout.SyscallWatcher)
	if *syscallCompleted {
		return
	}
	logrus.WithFields(context).
		WithField(logfields.Timeout, timeout.SyscallWatcher).
		Warning("Syscall did not complete within operation timeout. This may indicate a platform issue. If it appears to be making no forward progress, obtain the stacks and see if there is a syscall stuck in the platform API for a significant length of time.")
}
