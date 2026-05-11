//go:build !windows

package review

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

// TestExecRunner_TimeoutKillsProcessGroup asserts the "kill the
// process group" semantics: when the timeout fires, a child spawned
// by the `claude` stub (a grandchild of the runner) is also
// terminated, not left orphaned.
//
// Strategy: the stub script forks a long-running `sleep` in the
// background that inherits the parent's stdout pipe, then sleeps
// itself. On timeout, with process-group kill, the runner's cmd.Cancel
// sends SIGKILL to the whole group — grandchild dies, stdout pipe
// closes, cmd.Run returns immediately. Without process-group kill, the
// direct child dies but the grandchild keeps the stdout pipe open and
// cmd.Wait blocks until the grandchild's sleep elapses (30s). The
// elapsed-time assertion is the load-bearing one.
//
// On Windows the process-group plumbing is a no-op so this test is
// build-tagged off — see runner_windows.go for the rationale.
func TestExecRunner_TimeoutKillsProcessGroup(t *testing.T) {
	if _, err := lookSh(); err != nil {
		t.Skipf("no /bin/sh available: %v", err)
	}
	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("no sleep on PATH: %v", err)
	}

	dir := t.TempDir()
	// Stub: spawn a long sleep in the background (inheriting stdout),
	// then sleep ourselves so the runner's timeout has something to kill.
	// The background sleep is the grandchild that leaks without the
	// process-group kill.
	body := "#!/bin/sh\n" +
		sleepPath + " 30 &\n" +
		"exec " + sleepPath + " 30\n"
	if err := writeExec(dir, "claude", body); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	r := &ExecRunner{}
	start := time.Now()
	_, _, runErr := r.Run(ctx, "prompt")
	elapsed := time.Since(start)
	if !errors.Is(runErr, ErrTimeout) {
		t.Fatalf("got %v; want ErrTimeout", runErr)
	}
	// Run must return promptly after the timeout fires. Without the
	// process-group kill the grandchild keeps the parent's stdout pipe
	// open, so cmd.Wait blocks until the grandchild exits naturally —
	// here, 30 seconds. Cap at 3s to leave headroom for slow CI without
	// confusing "fix works" with "grandchild happened to be short".
	if elapsed > 3*time.Second {
		t.Fatalf("Run took %s after timeout — process group likely not killed (grandchild still holds stdout pipe)", elapsed)
	}
}
