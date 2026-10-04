package runner

import (
	"os"
	"path/filepath"
	"strings"
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

func TestRunChildUserNamespaceHint(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a local helper process")
	}
	for _, message := range []string{
		"bwrap: setting up uid map: Permission denied",
		"bwrap: creating new namespace failed: Operation not permitted",
	} {
		t.Run(message, func(t *testing.T) {
			bwrap := filepath.Join(t.TempDir(), "bwrap")
			script := "#!/bin/sh\nprintf '%s\\n' '" + message + "' >&2\nexit 1\n"
			if err := os.WriteFile(bwrap, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			code, err := runChild(bwrap, plan{env: os.Environ()})
			if code != 1 || err == nil || !strings.Contains(err.Error(), "docs/troubleshooting.md") {
				t.Fatalf("runChild = %d, %v; want exit 1 and troubleshooting hint", code, err)
			}
		})
	}
}

func TestRunChildDoesNotHintOnOtherFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a local helper process")
	}
	bwrap := filepath.Join(t.TempDir(), "bwrap")
	if err := os.WriteFile(bwrap, []byte("#!/bin/sh\nprintf '%s\\n' 'bwrap: invalid option' >&2\nexit 2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	code, err := runChild(bwrap, plan{env: os.Environ()})
	if code != 2 || err != nil {
		t.Fatalf("runChild = %d, %v; want 2, nil", code, err)
	}
}

func TestShellJoinQuotesArguments(t *testing.T) {
	got := shellJoin([]string{"bwrap", "plain", "has space", "it's"})
	want := "'bwrap' 'plain' 'has space' 'it'\\''s'"
	if got != want {
		t.Fatalf("shellJoin = %q; want %q", got, want)
	}
}
