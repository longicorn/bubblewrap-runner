package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func inDirectory(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Error(err)
		}
	})
}

func TestBuildPlanWorkspaceAndExplicitMounts(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cwd := filepath.Join(root, "project")
	shared := filepath.Join(root, "shared")
	secret := filepath.Join(root, "secret")
	for _, dir := range []string{home, cwd, shared, secret} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "no-config"))
	inDirectory(t, cwd)
	plan, err := buildPlan(options{ro: stringList{shared}, rw: stringList{shared}, deny: stringList{secret}, noNet: true}, []string{"echo", "hello world"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasSequence(plan.args, "--bind", cwd, cwd) {
		t.Fatal("workspace is not writable")
	}
	if !hasSequence(plan.args, "--bind", shared, shared) {
		t.Fatal("higher-ranked rw rule did not override ro rule")
	}
	if !hasSequence(plan.args, "--tmpfs", secret) {
		t.Fatal("explicit deny was not applied")
	}
	if !hasArg(plan.args, "--unshare-net") {
		t.Fatal("--no-net did not isolate network")
	}
	if !hasSequence(plan.args, "--chdir", cwd) {
		t.Fatal("sandbox does not start in workspace")
	}
	if !hasSequence(plan.args, "--", "echo", "hello world") {
		t.Fatal("command arguments were not preserved")
	}
	if lookup, ok := lookupEnv(plan.env, sandboxMarkerEnv); !ok || lookup != "1" {
		t.Fatalf("sandbox marker = %q, %v", lookup, ok)
	}
}

func TestBuildPlanRejectsHomeAsWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	inDirectory(t, home)
	if _, err := buildPlan(options{}, []string{"true"}); err == nil || !strings.Contains(err.Error(), "home directory") {
		t.Fatalf("buildPlan error = %v; want home workspace rejection", err)
	}
}

func TestBuildPlanProjectShadowMount(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cwd := filepath.Join(root, "project")
	shadow := filepath.Join(cwd, ".bwrun", "sandbox", ".ssh")
	if err := os.MkdirAll(shadow, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shadow, "id_test"), []byte("isolated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "absent"))
	inDirectory(t, cwd)
	plan, err := buildPlan(options{}, []string{"true"})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".ssh")
	if !hasSequence(plan.args, "--bind", shadow, target) {
		t.Fatalf("project shadow was not mounted at %s", target)
	}
}

func TestWithin(t *testing.T) {
	if !within("/home/user", "/home/user/project") {
		t.Fatal("child path not recognized")
	}
	if !within("/home/user", "/home/user") {
		t.Fatal("path should contain itself")
	}
	if within("/home/user", "/home/username/file") {
		t.Fatal("sibling prefix incorrectly treated as child")
	}
}

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
