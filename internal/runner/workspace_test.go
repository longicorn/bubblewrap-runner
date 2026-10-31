package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceBoundary(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(home, "project")
	child := filepath.Join(project, "child")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".git"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateWorkspace(child, home, nil, false); err == nil {
		t.Fatal("home marker authorized child")
	}
	if err := validateWorkspace(child, home, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".git"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateWorkspace(child, home, nil, false); err != nil {
		t.Fatal(err)
	}
	layers := []configLayer{{rank: 1, config: Config{WorkspaceMarkers: []string{".git"}}}, {rank: 2, config: Config{WorkspaceMarkers: []string{".hg"}}}}
	if err := validateWorkspace(child, home, layers, false); err == nil {
		t.Fatal("higher layer did not replace markers")
	}
	if err := os.WriteFile(filepath.Join(project, ".bwrun.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateWorkspace(child, home, layers, false); err != nil {
		t.Fatal(err)
	}
}

func TestProtectedWorkspaceEvenWithOverride(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	for _, dir := range []string{home, root, "/"} {
		inDirectory(t, dir)
		if _, err := buildPlan(options{allowCwd: true}, []string{"true"}); err == nil || !strings.Contains(err.Error(), "protected") {
			t.Fatalf("%s: %v", dir, err)
		}
	}
}

func TestWorkspaceMarkerConfigValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, value := range []string{`null`, `[]`, `[".."]`, `["a/b"]`, `["a\\b"]`, `[""]`, `".git"`} {
		if err := os.WriteFile(path, []byte(`{"workspace-markers":`+value+`}`), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readConfig(path); err == nil {
			t.Errorf("accepted %s", value)
		}
	}
}
