package runner

import (
	"os"
	"os/exec"
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

func TestBuildPlanLimitsAutomaticXDGWritableMountsToHome(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cwd := filepath.Join(root, "project")
	outside := filepath.Join(root, "outside")
	inside := filepath.Join(home, "xdg-cache")
	link := filepath.Join(home, "xdg-link")
	for _, dir := range []string{home, cwd, outside, inside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", outside)
	t.Setenv("XDG_CACHE_HOME", inside)
	t.Setenv("XDG_DATA_HOME", link)
	t.Setenv("XDG_STATE_HOME", outside)
	inDirectory(t, cwd)

	plan, err := buildPlan(options{}, []string{"true"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasSequence(plan.args, "--bind", inside, inside) {
		t.Fatal("XDG directory within home was not writable")
	}
	if hasSequence(plan.args, "--bind", outside, outside) || hasSequence(plan.args, "--bind", outside, link) {
		t.Fatal("XDG directory outside home was mounted writable automatically")
	}

	plan, err = buildPlan(options{rw: stringList{outside}}, []string{"true"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasSequence(plan.args, "--bind", outside, outside) {
		t.Fatal("explicit writable mount outside home was not applied")
	}
}

func TestBuildPlanDowngradesWritableParentOfMissingDeniedPath(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cwd := filepath.Join(root, "project")
	config := filepath.Join(home, ".config")
	for _, dir := range []string{home, cwd, config} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "absent-cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "absent-data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "absent-state"))
	inDirectory(t, cwd)

	plan, err := buildPlan(options{}, []string{"true"})
	if err != nil {
		t.Fatal(err)
	}
	if lastMountMode(plan.args, config) != "--ro-bind" {
		t.Fatal("XDG config stayed writable despite missing denied paths")
	}
	if _, err := os.Stat(filepath.Join(config, "autostart")); !os.IsNotExist(err) {
		t.Fatalf("planning created denied path on host: %v", err)
	}

	missing := filepath.Join(cwd, "blocked")
	plan, err = buildPlan(options{deny: stringList{missing}}, []string{"true"})
	if err != nil {
		t.Fatal(err)
	}
	if lastMountMode(plan.args, cwd) != "--ro-bind" {
		t.Fatal("workspace stayed writable despite missing explicit denial")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("planning created denied workspace path on host: %v", err)
	}

	readonly := filepath.Join(cwd, "readonly")
	if err := os.Mkdir(readonly, 0o755); err != nil {
		t.Fatal(err)
	}
	plan, err = buildPlan(options{ro: stringList{readonly}, deny: stringList{filepath.Join(readonly, "blocked")}}, []string{"true"})
	if err != nil {
		t.Fatal(err)
	}
	if lastMountMode(plan.args, cwd) != "--bind" {
		t.Fatal("writable workspace was downgraded despite a closer read-only mount")
	}
}

func TestMissingDeniedPathCannotBeCreatedInSandbox(t *testing.T) {
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("bubblewrap is not installed")
	}
	if err := exec.Command(bwrap, "--ro-bind", "/", "/", "--", "true").Run(); err != nil {
		t.Skipf("bubblewrap namespaces are unavailable: %v", err)
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cwd := filepath.Join(root, "project")
	config := filepath.Join(home, ".config")
	for _, dir := range []string{home, cwd, config} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "absent-cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "absent-data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "absent-state"))
	inDirectory(t, cwd)

	denied := filepath.Join(config, "autostart")
	plan, err := buildPlan(options{}, []string{"sh", "-c", "mkdir \"$HOME/.config/autostart\" 2>/dev/null; test ! -e \"$HOME/.config/autostart\""})
	if err != nil {
		t.Fatal(err)
	}
	code, err := runChild(bwrap, plan)
	if err != nil || code != 0 {
		t.Fatalf("sandbox command exited %d: %v", code, err)
	}
	if _, err := os.Stat(denied); !os.IsNotExist(err) {
		t.Fatalf("sandbox created denied path on host: %v", err)
	}
}

func TestBuildPlanRejectsSymbolicLinkPolicyPath(t *testing.T) {
	for _, name := range []string{".bwrun.local.json", ".bwrun"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			cwd := filepath.Join(root, "project")
			for _, dir := range []string{home, cwd} {
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(home, filepath.Join(cwd, name)); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "absent"))
			inDirectory(t, cwd)
			if _, err := buildPlan(options{}, []string{"true"}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
				t.Fatalf("buildPlan error = %v; want symbolic link rejection", err)
			}
		})
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
