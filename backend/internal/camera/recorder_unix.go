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

// sendInterrupt sends SIGINT to the ffmpeg process for graceful segment finalization.
func sendInterrupt(proc *os.Process, _ io.WriteCloser) error {
	return proc.Signal(syscall.SIGINT)
}
