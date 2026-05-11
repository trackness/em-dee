//go:build !windows

package review

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup installs the unix process-group plumbing per
// spec §7.6: when the context's deadline fires, the entire process
// group spawned for `claude` is killed, not just the direct child.
//
// Why this matters: `claude` may itself be a wrapper script (a Node
// shim, a venv launcher, etc.) that forks subprocesses. The default
// exec.CommandContext behaviour sends SIGKILL only to the direct child
// — any grandchildren leak after the deadline. Setting Setpgid means
// the child becomes its own process-group leader; cmd.Cancel then sends
// SIGKILL to -pid (the negation tells the kernel "every process whose
// pgid equals pid").
//
// Why override cmd.Cancel rather than relying on SysProcAttr alone:
// exec.CommandContext's default Cancel calls cmd.Process.Kill(), which
// sends a single SIGKILL to the leader only. We need the group-wide
// kill, so we replace Cancel with the syscall.Kill(-pid, SIGKILL) form.
//
// WaitDelay is left at zero (the default): cmd.Run waits for the
// process to exit after Cancel fires, which is what we want — by the
// time cmd.Run returns we know nothing in the group is still alive.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
