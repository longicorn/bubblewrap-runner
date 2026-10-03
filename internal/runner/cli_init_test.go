package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMainArgumentErrors(t *testing.T) {
	for _, args := range [][]string{{}, {"--unknown"}, {"init", "extra"}} {
		code, err := Main(args)
		if err == nil || code != 2 {
			t.Errorf("Main(%v) = %d, %v; want usage error", args, code, err)
		}
	}
}

func TestInitializeProjectCreatesTemplateAndPreservesExisting(t *testing.T) {
	cwd := t.TempDir()
	inDirectory(t, cwd)
	path := filepath.Join(cwd, ".bwrun.json")
	if err := initializeProject(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Version != "1" || cfg.Network != "allow" || cfg.Mounts.RO == nil || cfg.Mounts.RW == nil || cfg.Mounts.Deny == nil || cfg.Env.Pass == nil || cfg.Env.Set == nil {
		t.Fatalf("unexpected starter config: %#v", cfg)
	}
	if err := os.WriteFile(path, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := initializeProject(); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing config result = %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "keep me" {
		t.Fatalf("existing config changed: %q, %v", data, err)
	}
}
