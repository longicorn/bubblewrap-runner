package runner

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
)

func runChild(bwrap string, sandbox plan) (int, error) {
	cmd := exec.Command(bwrap, sandbox.args...)
	cmd.Env = sandbox.env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if needsEmptyFile(sandbox.args) {
		empty, err := os.Open(os.DevNull)
		if err != nil {
			return 1, fmt.Errorf("open null device: %w", err)
		}
		defer empty.Close()
		cmd.ExtraFiles = []*os.File{empty} // Bubblewrap sees this as file descriptor 3.
	}
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
			return status.ExitStatus(), nil
		}
	}
	return 1, fmt.Errorf("wait for bubblewrap: %w", err)
}

func needsEmptyFile(args []string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == "--file" && args[index+1] == "3" {
			return true
		}
	}
	return false
}

func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for index, arg := range args {
		quoted[index] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
	}
	return strings.Join(quoted, " ")
}
