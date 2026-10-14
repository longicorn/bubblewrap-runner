package runner

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCopilotSessionStateBinding(t *testing.T) {
	requireBubblewrap(t)
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if within("/tmp", workingDir) {
		t.Skip("checkout under /tmp is hidden by the sandbox's private /tmp")
	}
	for _, test := range []struct {
		name            string
		explicitConfig  bool
		hostStateExists bool
	}{
		{"project config with automatic overlay", false, true},
		{"profile outside workspace", true, true},
		{"absent host destination", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, err := os.MkdirTemp(workingDir, "bwrun-copilot-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(root) })
			home := filepath.Join(root, "home")
			project := filepath.Join(home, "project")
			profiles := filepath.Join(home, "profiles")
			hostCopilot := filepath.Join(home, ".copilot")
			hostState := filepath.Join(hostCopilot, "session-state")
			source := filepath.Join(project, ".bwrun", "sandbox", ".copilot", "session-state")
			for _, dir := range []string{source, profiles, hostCopilot} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(hostCopilot, "config.json"), []byte("host config"), 0o600); err != nil {
				t.Fatal(err)
			}
			if test.hostStateExists {
				if err := os.Mkdir(hostState, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(hostState, "host-session"), []byte("host history"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			config := filepath.Join(project, ".bwrun.json")
			flags := []string{}
			if test.explicitConfig {
				config = filepath.Join(profiles, "agent.json")
				flags = []string{"--config", "../profiles/agent.json"}
			}
			data, err := json.Marshal(Config{Mounts: MountConfig{RW: []RWMount{
				{Path: "~/.copilot"},
				{Source: ".bwrun/sandbox/.copilot/session-state", Target: "~/.copilot/session-state"},
			}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(config, data, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			for _, name := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
				t.Setenv(name, filepath.Join(home, "missing-"+name))
			}
			inDirectory(t, project)

			const write = `
set -eu
test "$(cat "$HOME/.copilot/config.json")" = "host config" || exit 10
test ! -e "$HOME/.copilot/session-state/host-session" || exit 11
printf 'updated host config' > "$HOME/.copilot/config.json"
printf 'project history' > "$HOME/.copilot/session-state/project-session"
if (printf tampered > "$1") 2>/dev/null; then exit 12; fi
`
			args := append(append([]string{}, flags...), "--", "/bin/bash", "-c", write, "binding-check", config)
			if code, err := Main(args); err != nil || code != 0 {
				t.Fatalf("session write exited %d: %v", code, err)
			}
			const read = `
set -eu
test "$(cat "$HOME/.copilot/config.json")" = "updated host config" || exit 20
test "$(cat "$HOME/.copilot/session-state/project-session")" = "project history" || exit 21
test ! -e "$HOME/.copilot/session-state/host-session" || exit 22
`
			args = append(append([]string{}, flags...), "--", "/bin/bash", "-c", read)
			if code, err := Main(args); err != nil || code != 0 {
				t.Fatalf("second invocation exited %d: %v", code, err)
			}
			if data, err := os.ReadFile(filepath.Join(source, "project-session")); err != nil || string(data) != "project history" {
				t.Fatalf("persisted project history = %q, %v", data, err)
			}
			if _, err := os.Stat(filepath.Join(hostState, "project-session")); !os.IsNotExist(err) {
				t.Fatalf("project history leaked into host sessions: %v", err)
			}
			if test.hostStateExists {
				if data, err := os.ReadFile(filepath.Join(hostState, "host-session")); err != nil || string(data) != "host history" {
					t.Fatalf("host history changed = %q, %v", data, err)
				}
			}
		})
	}
}

func requireBubblewrap(t *testing.T) string {
	t.Helper()
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
	return bwrap
}

// TestIsolationBoundary exercises the mount policy through a real bwrap process.
// Fixtures live beside the checkout so the private /tmp does not hide them.
func TestIsolationBoundary(t *testing.T) {
	bwrap := requireBubblewrap(t)
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

func TestExplicitSandboxDirectory(t *testing.T) {
	requireBubblewrap(t)
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if within("/tmp", workingDir) {
		t.Skip("checkout under /tmp is hidden by the sandbox's private /tmp")
	}
	root, err := os.MkdirTemp(workingDir, "bwrun-overlay-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	home := filepath.Join(root, "home")
	project := filepath.Join(home, "project")
	profiles := filepath.Join(home, "profiles")
	configured := filepath.Join(project, "sandbox")
	profileSandbox := filepath.Join(profiles, "sandbox")
	cli := filepath.Join(project, ".bwrun", "sandbox")
	for _, dir := range []string{filepath.Join(home, ".ssh"), filepath.Join(configured, ".ssh"), filepath.Join(cli, ".ssh"), filepath.Join(profileSandbox, ".ssh")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(home, ".ssh", "config"):           "host",
		filepath.Join(home, ".ssh", "host-only"):        "host secret",
		filepath.Join(configured, ".ssh", "config"):     "configured",
		filepath.Join(configured, ".overlay-file"):      "configured",
		filepath.Join(profileSandbox, ".ssh", "config"): "wrong-profile-directory",
		filepath.Join(profileSandbox, ".overlay-file"):  "wrong-profile-directory",
		filepath.Join(cli, ".ssh", "config"):            "cli",
		filepath.Join(cli, ".overlay-file"):             "cli",
		filepath.Join(profiles, "empty.json"):           `{}`,
		filepath.Join(profiles, "agent.json"):           `{"sandbox-dir":"sandbox"}`,
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
test ! -e "$HOME/.ssh/host-only" || exit 10
if test "$1" = hidden; then
  test ! -e "$HOME/.ssh/config" || exit 11
else
  test "$(cat "$HOME/.ssh/config")" = "$1" || exit 12
  test "$(cat "$HOME/.overlay-file")" = "$1" || exit 13
  printf changed > "$HOME/.ssh/config"
  printf changed > "$HOME/.overlay-file"
fi
`
	tests := []struct {
		name   string
		flags  []string
		want   string
		source string
	}{
		{"no implicit overlay with config", []string{"--config", "../profiles/empty.json"}, "hidden", ""},
		{"configured directory", []string{"--config", "../profiles/agent.json"}, "configured", configured},
		{"CLI overrides configured directory", []string{"--config", "../profiles/agent.json", "--sandbox-dir", ".bwrun/sandbox"}, "cli", cli},
		{"CLI mount policy overrides overlay", []string{"--config", "../profiles/agent.json", "--deny", "~/.ssh"}, "hidden", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := append(append([]string{}, test.flags...), "--", "/bin/bash", "-c", check, "overlay-check", test.want)
			code, err := Main(args)
			if err != nil || code != 0 {
				t.Fatalf("overlay check exited %d: %v", code, err)
			}
			if test.source != "" {
				for _, name := range []string{filepath.Join(".ssh", "config"), ".overlay-file"} {
					if data, err := os.ReadFile(filepath.Join(test.source, name)); err != nil || string(data) != "changed" {
						t.Errorf("overlay write to %s = %q, %v", name, data, err)
					}
				}
			}
		})
	}
	if data, err := os.ReadFile(filepath.Join(home, ".ssh", "config")); err != nil || string(data) != "host" {
		t.Fatalf("host SSH config changed: %q, %v", data, err)
	}
}
