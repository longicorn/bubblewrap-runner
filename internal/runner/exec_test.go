package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunChildPropagatesExitStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a local helper process")
	}
	bwrap := filepath.Join(t.TempDir(), "bwrap")
	if err := os.WriteFile(bwrap, []byte("#!/bin/sh\nexit 23\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	code, err := runChild(bwrap, plan{env: os.Environ()})
	if err != nil || code != 23 {
		t.Fatalf("runChild = %d, %v; want 23, nil", code, err)
	}
}

func TestRunChildReportsMissingExecutable(t *testing.T) {
	code, err := runChild(filepath.Join(t.TempDir(), "missing"), plan{})
	if err == nil || code != 127 {
		t.Fatalf("runChild = %d, %v; want start failure", code, err)
	}
}

func TestShellJoinQuotesArguments(t *testing.T) {
	got := shellJoin([]string{"bwrap", "plain", "has space", "it's"})
	want := "'bwrap' 'plain' 'has space' 'it'\\''s'"
	if got != want {
		t.Fatalf("shellJoin = %q; want %q", got, want)
	}
}
