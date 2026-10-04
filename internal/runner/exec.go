package runner

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"unsafe"
)

const maxCapturedStderr = 64 * 1024

type limitedCapture struct {
	data []byte
}

func (c *limitedCapture) Write(p []byte) (int, error) {
	remaining := maxCapturedStderr - len(c.data)
	if remaining > 0 {
		if len(p) < remaining {
			remaining = len(p)
		}
		c.data = append(c.data, p[:remaining]...)
	}
	return len(p), nil
}

func isUserNamespaceError(stderr string) bool {
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "bwrap:") {
			continue
		}
		if (strings.Contains(line, "setting up uid map") || strings.Contains(line, "creating new namespace failed")) &&
			(strings.Contains(line, "Permission denied") || strings.Contains(line, "Operation not permitted")) {
			return true
		}
	}
	return false
}

func runChild(bwrap string, sandbox plan) (int, error) {
	cmd := exec.Command(bwrap, sandbox.args...)
	cmd.Env = sandbox.env
	var stderr limitedCapture
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// Bash checks whether stderr is a terminal before enabling interactive mode.
	// A MultiWriter makes os/exec replace it with a pipe, even when the user
	// started bwrun from a terminal.
	if !isTerminal(os.Stderr) {
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	}
	files := make([]*os.File, 0, sandbox.fileCount)
	defer func() {
		for _, file := range files {
			_ = file.Close()
		}
	}()
	for index := 0; index < sandbox.fileCount; index++ {
		empty, err := os.Open(os.DevNull)
		if err != nil {
			for _, file := range files {
				_ = file.Close()
			}
			return 1, fmt.Errorf("open null device: %w", err)
		}
		files = append(files, empty)
	}
	cmd.ExtraFiles = files // ExtraFiles are mapped consecutively starting at fd 3.
	if err := cmd.Start(); err != nil {
		return 127, fmt.Errorf("start bubblewrap: %w", err)
	}
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for sig := range signals {
			_ = cmd.Process.Signal(sig)
		}
	}()
	err := cmd.Wait()
	signal.Stop(signals)
	close(signals)
	<-done
	if err == nil {
		return 0, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			if status.Signaled() {
				return 128 + int(status.Signal()), nil
			}
			if isUserNamespaceError(string(stderr.data)) {
				return status.ExitStatus(), fmt.Errorf("bubblewrap could not create a user namespace; see docs/troubleshooting.md for host permission settings")
			}
			return status.ExitStatus(), nil
		}
	}
	return 1, fmt.Errorf("wait for bubblewrap: %w", err)
}

func isTerminal(file *os.File) bool {
	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&termios)))
	return errno == 0
}

func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for index, arg := range args {
		quoted[index] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
	}
	return strings.Join(quoted, " ")
}
