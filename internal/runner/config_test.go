package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRWMountJSON(t *testing.T) {
	var cfg Config
	input := `{"mounts":{"rw":["~/.copilot",{"source":".bwrun/sandbox/.copilot/session-state","target":"~/.copilot/session-state"}]}}`
	if err := json.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Mounts.RW) != 2 || cfg.Mounts.RW[0].Path != "~/.copilot" || cfg.Mounts.RW[1].Source != ".bwrun/sandbox/.copilot/session-state" || cfg.Mounts.RW[1].Target != "~/.copilot/session-state" {
		t.Fatalf("mixed rw entries = %#v", cfg.Mounts.RW)
	}
	data, err := json.Marshal(cfg.Mounts.RW)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `["~/.copilot",{"source":".bwrun/sandbox/.copilot/session-state","target":"~/.copilot/session-state"}]` {
		t.Fatalf("rw round trip = %s", data)
	}
	for _, input := range []string{`null`, `true`, `[]`, `{}`, `{"source":"a"}`, `{"target":"b"}`, `{"source":" ","target":"b"}`, `{"source":1,"target":"b"}`, `{"source":"a","target":"b","typo":"c"}`} {
		t.Run(input, func(t *testing.T) {
			var rule RWMount
			if err := json.Unmarshal([]byte(input), &rule); err == nil {
				t.Fatalf("invalid binding accepted: %s", input)
			}
		})
	}
}

func TestExpandPath(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "tester")
	base := filepath.Join(string(filepath.Separator), "work", "project")
	tests := []struct{ value, want string }{
		{"~", home}, {"~/src", filepath.Join(home, "src")},
		{"relative/file", filepath.Join(base, "relative", "file")},
		{filepath.Join(string(filepath.Separator), "var", "tmp"), filepath.Join(string(filepath.Separator), "var", "tmp")},
	}
	for _, test := range tests {
		got, err := expandPath(test.value, base, home)
		if err != nil || got != test.want {
			t.Errorf("expandPath(%q) = %q, %v; want %q", test.value, got, err, test.want)
		}
	}
	t.Setenv("BWRUN_TEST_PATH", "chosen")
	got, err := expandPath("$BWRUN_TEST_PATH", base, home)
	if err != nil || got != filepath.Join(base, "chosen") {
		t.Fatalf("environment expansion = %q, %v", got, err)
	}
}

func TestReadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if _, ok, err := readConfig(path); err != nil || ok {
		t.Fatalf("missing config: ok=%v err=%v", ok, err)
	}
	if err := os.WriteFile(path, []byte(`{"version":"1","network":"deny","sandbox-dir":"sandbox","mounts":{"ro":["/tmp"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, ok, err := readConfig(path)
	if err != nil || !ok || cfg.Network != "deny" || cfg.SandboxDir != "sandbox" || len(cfg.Mounts.RO) != 1 {
		t.Fatalf("read config = %#v, %v, %v", cfg, ok, err)
	}
	for _, body := range []string{`{`, `{"network":"sometimes"}`, `{"sandbox-dir":true}`} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readConfig(path); err == nil {
			t.Errorf("readConfig accepted %q", body)
		}
	}
}

func TestReadConfigRejectsSymbolicLink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	if err := os.WriteFile(target, []byte(`{"network":"allow"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".bwrun.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readConfig(link); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("readConfig error = %v; want symbolic link rejection", err)
	}
}

func TestLoadConfigLayersUsesNearestProjectAndRanks(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	child := filepath.Join(project, "sub")
	for _, dir := range []string{home, child, filepath.Join(home, ".config", "bwrun")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	global := filepath.Join(home, ".config", "bwrun", "config.json")
	if err := os.WriteFile(global, []byte(`{"network":"deny"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".bwrun.json"), []byte(`{"network":"allow"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, ".bwrun.json"), []byte(`{"network":"deny"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	layers, err := loadConfigLayers(child, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 2 || layers[0].rank != 1 || layers[1].rank != 2 || layers[1].config.Network != "deny" {
		t.Fatalf("unexpected layers: %#v", layers)
	}
	if !strings.HasSuffix(layers[1].base, "sub") {
		t.Fatalf("nearest project base = %q", layers[1].base)
	}
}

func TestLoadExplicitConfigRequiresExistingFile(t *testing.T) {
	root := t.TempDir()
	if _, _, err := loadExplicitConfig("missing.json", root, root); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing explicit config error = %v", err)
	}
}
