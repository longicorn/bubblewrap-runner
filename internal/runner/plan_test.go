package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPlanRWBinding(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(home, "project")
	profiles := filepath.Join(home, "profiles")
	source := filepath.Join(project, ".bwrun", "sandbox", "state")
	for _, dir := range []string{source, profiles} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	profile := filepath.Join(profiles, "agent.json")
	file := filepath.Join(project, "file")
	if err := os.WriteFile(file, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	inDirectory(t, project)
	valid := RWMount{Source: ".bwrun/sandbox/state", Target: "~/.new-state"}
	tests := []struct {
		name    string
		rule    RWMount
		deny    bool
		wantErr string
	}{
		{name: "cwd source and absent target", rule: valid},
		{name: "CLI deny overrides absent host target", rule: valid, deny: true},
		{name: "missing source", rule: RWMount{Source: "missing", Target: "~/.new-state"}, wantErr: "resolve binding source"},
		{name: "protected home target", rule: RWMount{Source: valid.Source, Target: "~"}, wantErr: "protected root or home"},
		{name: "protected root target", rule: RWMount{Source: valid.Source, Target: "/"}, wantErr: "protected root or home"},
		{name: "protected home source", rule: RWMount{Source: "~", Target: "~/.new-state"}, wantErr: "protected root or home"},
		{name: "different types", rule: RWMount{Source: valid.Source, Target: file}, wantErr: "types differ"},
		{name: "policy file alias", rule: RWMount{Source: profile, Target: "~/.policy-alias"}, wantErr: "protected policy file"},
		{name: "policy directory alias", rule: RWMount{Source: profiles, Target: "~/.policy-alias"}, wantErr: "protected policy file"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, err := json.Marshal(Config{Mounts: MountConfig{RW: []RWMount{test.rule}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(profile, data, 0o600); err != nil {
				t.Fatal(err)
			}
			opts := options{configPath: profile}
			if test.deny {
				opts.deny = stringList{valid.Target}
			}
			plan, err := buildPlan(opts, []string{"true"})
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("buildPlan error = %v; want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(home, ".new-state")
			if test.deny {
				if !hasSequence(plan.args, "--tmpfs", target) || hasSequence(plan.args, "--bind", source, target) {
					t.Fatal("CLI deny did not replace mapped directory")
				}
			} else if !hasSequence(plan.args, "--bind", source, target) {
				t.Fatal("relative source was not resolved from cwd")
			}
		})
	}
}

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

func TestBuildPlanExplicitConfigIgnoresDiscoveredConfigsAndProjectSandbox(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cwd := filepath.Join(home, "project")
	profileDir := filepath.Join(home, "profiles")
	globalDir := filepath.Join(home, ".config", "bwrun")
	shadow := filepath.Join(cwd, ".bwrun", "sandbox", ".ssh")
	for _, dir := range []string{profileDir, globalDir, shadow} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		filepath.Join(globalDir, "config.json"): `{"env":{"set":{"BWRUN_GLOBAL_ONLY":"yes"}}}`,
		filepath.Join(cwd, ".bwrun.json"):       `{"env":{"set":{"BWRUN_PROJECT_ONLY":"yes"}}}`,
		filepath.Join(profileDir, "agent.json"): `{"env":{"deny":["BWRUN_SECRET"],"set":{"BWRUN_PROFILE_ONLY":"yes"}}}`,
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("BWRUN_SECRET", "secret")
	inDirectory(t, cwd)
	sandbox, err := buildPlan(options{configPath: "../profiles/agent.json"}, []string{"true"})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := lookupEnv(sandbox.env, "BWRUN_PROFILE_ONLY"); !ok || got != "yes" {
		t.Fatalf("profile value = %q, %v", got, ok)
	}
	for _, name := range []string{"BWRUN_GLOBAL_ONLY", "BWRUN_PROJECT_ONLY", "BWRUN_SECRET"} {
		if _, ok := lookupEnv(sandbox.env, name); ok {
			t.Errorf("unexpected environment variable %s", name)
		}
	}
	if hasSequence(sandbox.args, "--bind", shadow, filepath.Join(home, ".ssh")) {
		t.Fatal("discovered project sandbox was mounted with explicit config")
	}
	if !hasSequence(sandbox.args, "--ro-bind", "/dev/null", filepath.Join(profileDir, "agent.json")) {
		t.Fatal("explicit config is not hidden inside the sandbox")
	}
}

func TestFindSandboxDirSelection(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(home, "project")
	profile := filepath.Join(home, "profiles")
	automatic := filepath.Join(project, ".bwrun", "sandbox")
	global := filepath.Join(project, "global-sandbox")
	local := filepath.Join(project, "local-sandbox")
	explicit := filepath.Join(project, "profile-sandbox")
	homeSandbox := filepath.Join(home, "global-sandbox")
	for _, dir := range []string{automatic, global, local, explicit, homeSandbox} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	globalLayer := configLayer{base: home, rank: 1, config: Config{SandboxDir: "global-sandbox"}}
	projectLayer := configLayer{base: filepath.Dir(project), rank: 2, config: Config{SandboxDir: "local-sandbox"}}
	profileLayer := configLayer{base: profile, rank: 2, config: Config{SandboxDir: "profile-sandbox"}}
	tests := []struct {
		name    string
		opts    options
		layers  []configLayer
		want    string
		wantErr string
	}{
		{name: "automatic", want: automatic},
		{name: "explicit config disables discovery", opts: options{configPath: "agent.json"}},
		{name: "global relative to cwd", layers: []configLayer{globalLayer}, want: global},
		{name: "project overrides global", layers: []configLayer{projectLayer, globalLayer}, want: local},
		{name: "omitted project value preserves global", layers: []configLayer{globalLayer, {base: project, rank: 2}}, want: global},
		{name: "profile relative to cwd", opts: options{configPath: "agent.json"}, layers: []configLayer{profileLayer}, want: explicit},
		{name: "CLI overrides config", opts: options{configPath: "agent.json", sandboxDir: ".bwrun/sandbox"}, layers: []configLayer{profileLayer}, want: automatic},
		{name: "home expansion", opts: options{sandboxDir: "~/global-sandbox"}, want: homeSandbox},
		{name: "absolute configured path", layers: []configLayer{{base: profile, rank: 2, config: Config{SandboxDir: homeSandbox}}}, want: homeSandbox},
		{name: "missing explicit directory", opts: options{sandboxDir: "missing"}, wantErr: "inspect sandbox directory"},
		{name: "missing configured directory", layers: []configLayer{{base: profile, rank: 2, config: Config{SandboxDir: "missing"}}}, wantErr: "inspect sandbox directory"},
		{name: "file", opts: options{sandboxDir: file}, wantErr: "not a directory"},
		{name: "blank", layers: []configLayer{{rank: 2, config: Config{SandboxDir: " "}}}, wantErr: "must not be empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, found, err := findSandboxDir(project, home, test.opts, test.layers)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("findSandboxDir = %q, %v, %v; want error %q", got, found, err, test.wantErr)
				}
				return
			}
			if err != nil || got != test.want || found != (test.want != "") {
				t.Fatalf("findSandboxDir = %q, %v, %v; want %q", got, found, err, test.want)
			}
		})
	}
}

func TestBuildPlanRejectsOverlayContainingSelectedConfig(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	profiles := filepath.Join(root, "profiles")
	for _, dir := range []string{home, project, profiles} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	profile := filepath.Join(profiles, "agent.json")
	if err := os.WriteFile(profile, []byte(`{"sandbox-dir":"../profiles"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	inDirectory(t, project)
	if _, err := buildPlan(options{configPath: profile}, []string{"true"}); err == nil || !strings.Contains(err.Error(), "must not contain the selected configuration file") {
		t.Fatalf("buildPlan error = %v; want config alias rejection", err)
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
