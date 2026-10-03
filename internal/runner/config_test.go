package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	if err := os.WriteFile(path, []byte(`{"version":"1","network":"deny","mounts":{"ro":["/tmp"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, ok, err := readConfig(path)
	if err != nil || !ok || cfg.Network != "deny" || len(cfg.Mounts.RO) != 1 {
		t.Fatalf("read config = %#v, %v, %v", cfg, ok, err)
	}
	for _, body := range []string{`{`, `{"network":"sometimes"}`} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readConfig(path); err == nil {
			t.Errorf("readConfig accepted %q", body)
		}
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
