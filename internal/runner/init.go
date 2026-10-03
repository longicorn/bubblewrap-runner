package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func initializeProject() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get current directory: %w", err)
	}
	path := filepath.Join(cwd, ".bwrun.json")
	config := Config{
		Schema:  "https://json-schema.org/draft/2020-12/schema",
		Version: "1",
		Network: "allow",
		Mounts:  MountConfig{RO: []string{}, RW: []string{}, Deny: []string{}},
		Env:     EnvConfig{Pass: []string{}, Set: map[string]string{}},
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode project config: %w", err)
	}
	data = append(data, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%s already exists; leaving it unchanged", path)
		}
		return fmt.Errorf("create project config %s: %w", path, err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("write project config %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("close project config %s: %w", path, err)
	}
	fmt.Fprintf(os.Stdout, "Created %s\n", path)
	fmt.Fprintln(os.Stdout, "Add paths under mounts.ro, mounts.rw, or mounts.deny, then run commands with bwrun.")
	return nil
}
