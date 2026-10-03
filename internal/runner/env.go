package runner

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

const sandboxMarkerEnv = "BWRUN_SANDBOX"

var validEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func buildEnvironment(layers []configLayer, home, cwd, sandboxPath string) ([]string, error) {
	pass := map[string]bool{
		"PATH": true, "TERM": true, "LANG": true, "LC_ALL": true,
		"LC_CTYPE": true, "USER": true, "LOGNAME": true, "SHELL": true,
		"XDG_CONFIG_HOME": true, "XDG_CACHE_HOME": true,
		"XDG_DATA_HOME": true, "XDG_STATE_HOME": true,
	}
	values := map[string]string{}
	ranks := map[string]int{}
	for _, layer := range layers {
		for _, name := range layer.config.Env.Pass {
			if !validEnvName.MatchString(name) {
				return nil, fmt.Errorf("invalid environment variable name %q", name)
			}
			pass[name] = true
		}
		for name, value := range layer.config.Env.Set {
			if !validEnvName.MatchString(name) {
				return nil, fmt.Errorf("invalid environment variable name %q", name)
			}
			if name == "HOME" || name == "PWD" || name == sandboxMarkerEnv {
				return nil, fmt.Errorf("%s is managed by bwrun and cannot be overridden", name)
			}
			if layer.rank >= ranks[name] {
				values[name], ranks[name] = value, layer.rank
			}
		}
	}
	for name := range pass {
		if name == "HOME" || name == "PWD" {
			continue
		}
		if name == "PATH" {
			if _, overridden := values[name]; !overridden && sandboxPath != "" {
				values[name] = sandboxPath
			}
			continue
		}
		if _, overridden := values[name]; overridden {
			continue
		}
		if value, ok := os.LookupEnv(name); ok {
			values[name] = value
		}
	}
	values["HOME"] = home
	values["PWD"] = cwd
	values[sandboxMarkerEnv] = "1"
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	env := make([]string, 0, len(names))
	for _, name := range names {
		env = append(env, name+"="+values[name])
	}
	return env, nil
}

func envNames(env []string) []string {
	names := make([]string, 0, len(env))
	for _, entry := range env {
		if name, _, ok := strings.Cut(entry, "="); ok {
			names = append(names, name)
		}
	}
	return names
}
