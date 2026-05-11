package review

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// lookSh reports whether /bin/sh is available. The exec-runner tests
// use a shell-script stub on PATH to exercise the subprocess, which
// only works on POSIX-y platforms; on Windows the tests skip.
func lookSh() (string, error) {
	if p, err := exec.LookPath("sh"); err == nil {
		return p, nil
	}
	if _, err := os.Stat("/bin/sh"); err == nil {
		return "/bin/sh", nil
	}
	return "", errors.New("/bin/sh not found")
}

// writeExec writes a shell script to dir/name and chmods it executable.
// Mode 0o755 so the test process can spawn it.
func writeExec(dir, name, body string) error {
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		return err
	}
	return nil
}
