package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type MountConfig struct {
	RO   []string `json:"ro"`
	RW   []string `json:"rw"`
	Deny []string `json:"deny"`
}

type EnvConfig struct {
	Pass []string          `json:"pass"`
	Deny []string          `json:"deny"`
	Set  map[string]string `json:"set"`
}

type Config struct {
	Schema  string      `json:"$schema,omitempty"`
	Version string      `json:"version"`
	Network string      `json:"network"`
	Mounts  MountConfig `json:"mounts"`
	Env     EnvConfig   `json:"env"`
}

type configLayer struct {
	config Config
	base   string
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
		layers = append(layers, configLayer{config: cfg, base: home, rank: 1})
	}

	for dir := cwd; ; dir = filepath.Dir(dir) {
		projectFile := filepath.Join(dir, ".bwrun.json")
		if cfg, ok, err := readConfig(projectFile); err != nil {
			return nil, err
		} else if ok {
			layers = append(layers, configLayer{config: cfg, base: dir, rank: 2})
			break
		}
		if dir == home || filepath.Dir(dir) == dir {
			break
		}
	}
	return layers, nil
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
