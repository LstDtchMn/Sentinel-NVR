//go:build !windows

package camera

import (
	"io"
	"os"
	"os/exec"
	"syscall"
)

// setSysProcAttr sets process group on Unix so we can signal ffmpeg directly.
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// sendInterrupt sends SIGINT to the ffmpeg process group for graceful segment finalization.
// Negative PID signals the entire process group (set by Setpgid: true in setSysProcAttr),
// ensuring any child processes spawned by ffmpeg also receive the signal.
func sendInterrupt(proc *os.Process, _ io.WriteCloser) error {
	return syscall.Kill(-proc.Pid, syscall.SIGINT)
}
