package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestIsolationBoundary exercises the mount policy through a real bwrap process.
// Fixtures live beside the checkout so the private /tmp does not hide them.
func TestIsolationBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("requires bubblewrap and user namespaces")
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("bubblewrap is not installed")
	}
	probe := exec.Command(bwrap, "--ro-bind", "/", "/", "--", "/bin/true")
	if output, err := probe.CombinedOutput(); err != nil {
		if isUserNamespaceError(string(output)) {
			t.Skipf("user namespaces are unavailable: %s", output)
		}
		t.Fatalf("bubblewrap probe failed: %v: %s", err, output)
	}

	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if within("/tmp", workingDir) {
		t.Skip("checkout under /tmp is hidden by the sandbox's private /tmp")
	}
	root, err := os.MkdirTemp(workingDir, "bwrun-isolation-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	home := filepath.Join(root, "home")
	project := filepath.Join(home, "project")
	other := filepath.Join(home, "other-project")
	ssh := filepath.Join(home, ".ssh")
	outside := filepath.Join(root, "outside")
	for _, dir := range []string{project, other, ssh, outside} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(project, ".bwrun.json"): `{}`,
		filepath.Join(other, "secret.txt"):    "other project",
		filepath.Join(ssh, "id_test"):         "host credential",
		filepath.Join(outside, "public.txt"):  "read-only root",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		t.Setenv(name, filepath.Join(home, "missing-"+name))
	}
	inDirectory(t, project)

	const check = `
set -eu
test "$PWD" = "$1" || exit 10
test "$HOME" = "$2" || exit 11
test ! -e "$3/secret.txt" || exit 12
test ! -e "$2/.ssh/id_test" || exit 13
test "$(cat "$4/public.txt")" = "read-only root" || exit 14
if (printf changed > "$4/public.txt") 2>/dev/null; then exit 15; fi
printf created > "$1/created.txt"
if (printf changed > "$1/.bwrun.json") 2>/dev/null; then exit 16; fi
`
	sandbox, err := buildPlan(options{}, []string{"/bin/sh", "-c", check, "isolation-check", project, home, other, outside})
	if err != nil {
		t.Fatal(err)
	}
	code, err := runChild(bwrap, sandbox)
	if err != nil || code != 0 {
		t.Fatalf("isolation check exited %d: %v", code, err)
	}
	if data, err := os.ReadFile(filepath.Join(project, "created.txt")); err != nil || string(data) != "created" {
		t.Fatalf("workspace write = %q, %v", data, err)
	}
	for _, path := range []string{filepath.Join(project, ".bwrun.json"), filepath.Join(outside, "public.txt")} {
		if data, err := os.ReadFile(path); err != nil || string(data) != files[path] {
			t.Errorf("host file %s changed: %q, %v", path, data, err)
		}
	}
}
