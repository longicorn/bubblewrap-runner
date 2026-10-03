package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestADR0009Plan(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "project")
	customConfig := filepath.Join(home, ".custom-config")
	for _, path := range []string{
		cwd,
		filepath.Join(cwd, ".bwrun"),
		filepath.Join(home, ".config", "autostart"),
		filepath.Join(home, ".config", "systemd", "user"),
		filepath.Join(customConfig, "autostart"),
		filepath.Join(customConfig, "systemd", "user"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{".bwrun.json", ".bwrun.local.json"} {
		if err := os.WriteFile(filepath.Join(cwd, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", customConfig)
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	policyDir := filepath.Join(cwd, ".bwrun")
	plan, err := buildPlan(options{rw: stringList{policyDir}}, []string{"true"})
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--unshare-pid", "--unshare-ipc"} {
		if !hasArg(plan.args, flag) {
			t.Errorf("missing %s", flag)
		}
	}
	if !hasSequence(plan.args, "--proc", "/proc") {
		t.Error("missing private /proc mount")
	}
	for _, name := range []string{".bwrun.json", ".bwrun.local.json", ".bwrun"} {
		path := filepath.Join(cwd, name)
		if !hasSequence(plan.args, "--ro-bind", path, path) {
			t.Errorf("policy path %s is not read-only", path)
		}
	}
	if lastMountMode(plan.args, policyDir) != "--ro-bind" {
		t.Error("explicit writable rule overrode runner policy protection")
	}
	for _, path := range []string{
		filepath.Join(home, ".config", "autostart"),
		filepath.Join(home, ".config", "systemd", "user"),
		filepath.Join(customConfig, "autostart"),
		filepath.Join(customConfig, "systemd", "user"),
	} {
		if !hasSequence(plan.args, "--tmpfs", path) {
			t.Errorf("persistence path %s is not hidden", path)
		}
	}
}

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func hasSequence(args []string, want ...string) bool {
	for i := 0; i+len(want) <= len(args); i++ {
		match := true
		for j, item := range want {
			if args[i+j] != item {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func lastMountMode(args []string, target string) string {
	mode := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--bind", "--ro-bind":
			if i+2 < len(args) && args[i+2] == target {
				mode = args[i]
			}
		}
	}
	return mode
}
