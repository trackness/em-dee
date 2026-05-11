//go:build windows

package review

import "os/exec"

// configureProcessGroup is a no-op on Windows. The "kill the process
// group" semantics map to JobObjects on Windows rather than the POSIX
// setpgid + signal-by-negative-pid dance, and JobObjects are out of
// scope for em-dee's review pipeline. The default exec.CommandContext
// behaviour (SIGKILL to the direct child only) remains in effect.
//
// If a Windows user reports leaked grandchildren of `claude.cmd`, this
// stub is the place to add a JobObject wiring — see
// https://pkg.go.dev/golang.org/x/sys/windows for the primitives.
func configureProcessGroup(_ *exec.Cmd) {}
