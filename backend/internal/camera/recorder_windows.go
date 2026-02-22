//go:build windows

package camera

import (
	"io"
	"os"
	"os/exec"
)

// setSysProcAttr is a no-op on Windows (no process group support via Setpgid).
func setSysProcAttr(cmd *exec.Cmd) {}

// sendInterrupt writes 'q' to ffmpeg's stdin for graceful shutdown on Windows.
// ffmpeg reads interactive commands from stdin; 'q' triggers a clean exit
// that finalizes the current segment.
func sendInterrupt(_ *os.Process, stdin io.WriteCloser) error {
	if stdin != nil {
		_, err := stdin.Write([]byte("q\n"))
		return err
	}
	return os.ErrProcessDone
}
