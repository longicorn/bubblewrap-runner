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
	pass := map[string]bool{}
	denied := map[string]bool{}
	values := map[string]string{}
	ranks := map[string]int{}
	for _, layer := range layers {
		for _, name := range layer.config.Env.Pass {
			if !validEnvName.MatchString(name) {
				return nil, fmt.Errorf("invalid environment variable name %q", name)
			}
			pass[name] = true
		}
		for _, name := range layer.config.Env.Deny {
			if !validEnvName.MatchString(name) {
				return nil, fmt.Errorf("invalid environment variable name %q", name)
			}
			denied[name] = true
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
	for _, entry := range os.Environ() {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || denied[name] {
			continue
		}
		values[name] = value
	}
	for name := range pass {
		if denied[name] {
			continue
		}
		if _, exists := values[name]; !exists {
			if value, ok := os.LookupEnv(name); ok {
				values[name] = value
			}
		}
	}
	if sandboxPath != "" {
		if _, overridden := values["PATH"]; !overridden {
			values["PATH"] = sandboxPath
		}
	}
	for name := range denied {
		delete(values, name)
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
