package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type MountConfig struct {
	RO   []string  `json:"ro"`
	RW   []RWMount `json:"rw"`
	Deny []string  `json:"deny"`
}

// RWMount retains the string form for same-path mounts and accepts a
// source/target object when a writable path needs a different destination.
type RWMount struct {
	Path   string `json:"-"`
	Source string `json:"source"`
	Target string `json:"target"`
}

func (m *RWMount) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '"' {
		var path string
		if err := json.Unmarshal(data, &path); err != nil {
			return err
		}
		*m = RWMount{Path: path}
		return nil
	}
	if len(data) == 0 || data[0] != '{' {
		return fmt.Errorf("mounts.rw entries must be paths or source/target objects")
	}
	type binding RWMount
	var value binding
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("mounts.rw binding: %w", err)
	}
	if strings.TrimSpace(value.Source) == "" || strings.TrimSpace(value.Target) == "" {
		return fmt.Errorf("mounts.rw binding requires non-empty source and target")
	}
	*m = RWMount(value)
	return nil
}

func (m RWMount) MarshalJSON() ([]byte, error) {
	if m.Source == "" && m.Target == "" {
		return json.Marshal(m.Path)
	}
	type binding RWMount
	return json.Marshal(binding(m))
}

type EnvConfig struct {
	Pass []string          `json:"pass"`
	Deny []string          `json:"deny"`
	Set  map[string]string `json:"set"`
}

type Config struct {
	Schema     string      `json:"$schema,omitempty"`
	Version    string      `json:"version"`
	Network    string      `json:"network"`
	SandboxDir string      `json:"sandbox-dir,omitempty"`
	Mounts     MountConfig `json:"mounts"`
	Env        EnvConfig   `json:"env"`
}

type configLayer struct {
	config Config
	base   string
	path   string
	rank   int
}

func configuredEnv(name string, layers []configLayer) string {
	value := os.Getenv(name)
	rank := 0
	for _, layer := range layers {
		if configured, ok := layer.config.Env.Set[name]; ok && layer.rank >= rank {
			value, rank = configured, layer.rank
		}
	}
	return value
}

func loadConfigLayers(cwd, home string) ([]configLayer, error) {
	var layers []configLayer
	globalDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("find user config directory: %w", err)
	}
	globalFile := filepath.Join(globalDir, "bwrun", "config.json")
	if cfg, ok, err := readConfig(globalFile); err != nil {
		return nil, err
	} else if ok {
		layers = append(layers, configLayer{config: cfg, base: home, path: globalFile, rank: 1})
	}

	for dir := cwd; ; dir = filepath.Dir(dir) {
		projectFile := filepath.Join(dir, ".bwrun.json")
		if cfg, ok, err := readConfig(projectFile); err != nil {
			return nil, err
		} else if ok {
			layers = append(layers, configLayer{config: cfg, base: dir, path: projectFile, rank: 2})
			break
		}
		if dir == home || filepath.Dir(dir) == dir {
			break
		}
	}
	return layers, nil
}

func loadExplicitConfig(value, cwd, home string) ([]configLayer, string, error) {
	path, err := expandPath(value, cwd, home)
	if err != nil {
		return nil, "", err
	}
	cfg, ok, err := readConfig(path)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, "", fmt.Errorf("config %s does not exist", path)
	}
	return []configLayer{{config: cfg, base: filepath.Dir(path), path: path, rank: 2}}, path, nil
}

func readConfig(path string) (Config, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, fmt.Errorf("inspect config %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Config{}, false, fmt.Errorf("config %s must not be a symbolic link", path)
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, false, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.Network != "" && cfg.Network != "allow" && cfg.Network != "deny" {
		return Config{}, false, fmt.Errorf("config %s: network must be \"allow\" or \"deny\"", path)
	}
	return cfg, true, nil
}

func expandPath(value, base, home string) (string, error) {
	value = os.ExpandEnv(value)
	if value == "~" {
		value = home
	} else if strings.HasPrefix(value, "~/") {
		value = filepath.Join(home, value[2:])
	} else if !filepath.IsAbs(value) {
		value = filepath.Join(base, value)
	}
	path, err := filepath.Abs(filepath.Clean(value))
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", value, err)
	}
	return path, nil
}
