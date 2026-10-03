package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type mount struct {
	path    string
	source  string
	mode    string
	rank    int
	missing bool
}

type plan struct {
	args      []string
	env       []string
	fileCount int
}

func buildPlan(opts options, command []string) (plan, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return plan{}, fmt.Errorf("get current directory: %w", err)
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return plan{}, fmt.Errorf("resolve current directory: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return plan{}, fmt.Errorf("find home directory: %w", err)
	}
	home, err = filepath.Abs(home)
	if err != nil {
		return plan{}, err
	}
	home, err = filepath.EvalSymlinks(home)
	if err != nil {
		return plan{}, fmt.Errorf("resolve home directory: %w", err)
	}
	if cwd == home {
		return plan{}, fmt.Errorf("starting in the home directory would expose it as the writable workspace; change to a project directory first")
	}
	layers, err := loadConfigLayers(cwd, home)
	if err != nil {
		return plan{}, err
	}
	selected := map[string]mount{}
	netDenied := false
	netRank := 0
	add := func(value, base, mode string, rank int, requireExisting bool) error {
		path, err := expandPath(value, base, home)
		if err != nil {
			return err
		}
		if path == "/" || path == home {
			return fmt.Errorf("refusing to mount protected path %s", path)
		}
		resolved := path
		target := path
		info, statErr := os.Stat(path)
		if statErr != nil {
			if os.IsNotExist(statErr) && !requireExisting && mode != "deny" {
				return nil
			}
			if !os.IsNotExist(statErr) || requireExisting {
				return fmt.Errorf("path %s: %w", path, statErr)
			}
		}
		if statErr == nil && (mode == "ro" || mode == "rw") {
			resolved, err = filepath.EvalSymlinks(path)
			if err != nil {
				return fmt.Errorf("resolve path %s: %w", path, err)
			}
			if info.IsDir() != isDir(resolved) {
				return fmt.Errorf("invalid mount source %s", path)
			}
			for _, parent := range selected {
				if (parent.mode == "ro" || parent.mode == "rw") && parent.path != path && within(parent.path, path) {
					target = resolved
					break
				}
			}
		} else if statErr == nil && mode == "deny" {
			if resolved, resolveErr := filepath.EvalSymlinks(path); resolveErr == nil {
				for _, parent := range selected {
					if (parent.mode == "ro" || parent.mode == "rw") && parent.path != path && within(parent.path, path) {
						target = resolved
						break
					}
				}
			}
		}
		candidate := mount{path: target, source: resolved, mode: mode, rank: rank, missing: os.IsNotExist(statErr)}
		if old, ok := selected[target]; !ok || rank >= old.rank {
			selected[target] = candidate
		}
		return nil
	}

	// Default workspace access has the lowest precedence and may be narrowed by policy.
	if err := add(cwd, cwd, "rw", 0, true); err != nil {
		return plan{}, err
	}
	if entries, err := os.ReadDir(home); err == nil {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") {
				if err := add(entry.Name(), home, "ro", -3, false); err != nil {
					return plan{}, err
				}
			}
		}
	}
	for _, path := range []string{
		"~/go/pkg/mod", "~/go/pkg/sumdb",
		"~/.npm", "~/.pnpm-store", "~/.yarn",
		"~/.cargo/registry", "~/.cargo/git",
		"~/.gem", "~/.gradle", "~/.m2/repository",
		"~/.bundle/cache",
	} {
		if err := add(path, home, "rw", -2, false); err != nil {
			return plan{}, err
		}
	}
	xdgPaths := map[string]string{}
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		value := configuredEnv(name, layers)
		if value == "" {
			switch name {
			case "XDG_CONFIG_HOME":
				value = "~/.config"
			case "XDG_CACHE_HOME":
				value = "~/.cache"
			case "XDG_DATA_HOME":
				value = "~/.local/share"
			case "XDG_STATE_HOME":
				value = "~/.local/state"
			}
		}
		path, err := expandPath(value, home, home)
		if err != nil {
			return plan{}, err
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return plan{}, fmt.Errorf("inspect %s path %s: %w", name, path, err)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return plan{}, fmt.Errorf("resolve %s path %s: %w", name, path, err)
		}
		if resolved == home || !within(home, resolved) {
			continue
		}
		if err := add(value, home, "rw", -2, false); err != nil {
			return plan{}, err
		}
		xdgPaths[name] = value
	}
	for _, path := range []string{
		"~/go",
		"~/.config/autostart", "~/.config/systemd/user",
		"~/.ssh", "~/.gnupg", "~/.aws", "~/.azure", "~/.kube", "~/.pki/nssdb",
		"~/.docker/config.json", "~/.config/gcloud", "~/.config/gh/hosts.yml", "~/.config/gh/hosts.yaml",
		"~/.config/gcloud/application_default_credentials.json", "~/.config/containers/auth.json", "~/.config/goose/credentials.yaml",
		"~/.config/opencode/auth.json", "~/.local/share/opencode/auth.json", "~/.continue/config.json",
		"~/.config/rclone/rclone.conf", "~/.config/sops/age/keys.txt", "~/.codex/auth.json", "~/.aider.conf.yml",
		"~/.cargo/credentials", "~/.cargo/credentials.toml", "~/.yarnrc", "~/.yarnrc.yml",
		"~/.config/pip/pip.conf", "~/.config/pypoetry/auth.toml", "~/.config/uv/uv.toml",
		"~/.gem/credentials", "~/.git-credentials", "~/.netrc", "~/.npmrc", "~/.pypirc",
		"~/.env", "~/.envrc", "~/.vault-token", "~/.config/age/keys.txt",
		"~/.bash_history", "~/.zsh_history", "~/.python_history", "~/.lesshst", "~/.gradle/gradle.properties",
		"~/.claude/.credentials.json", "~/.claude.json", "~/.gemini/oauth_creds.json",
		"~/.m2/settings.xml", "~/.bundle/config", "~/.terraform.d/credentials.tfrc.json",
		"~/.local/share/keyrings", "~/.local/share/gnome-keyring",
	} {
		if err := add(path, home, "deny", -1, false); err != nil {
			return plan{}, err
		}
	}
	for _, item := range []struct{ name, relative string }{
		{"XDG_CONFIG_HOME", "autostart"}, {"XDG_CONFIG_HOME", "systemd/user"},
		{"XDG_CONFIG_HOME", "gcloud"}, {"XDG_CONFIG_HOME", "gh/hosts.yml"},
		{"XDG_CONFIG_HOME", "gh/hosts.yaml"}, {"XDG_CONFIG_HOME", "containers/auth.json"},
		{"XDG_CONFIG_HOME", "goose/credentials.yaml"}, {"XDG_CONFIG_HOME", "opencode/auth.json"},
		{"XDG_CONFIG_HOME", "pip/pip.conf"}, {"XDG_CONFIG_HOME", "pypoetry/auth.toml"},
		{"XDG_CONFIG_HOME", "uv/uv.toml"}, {"XDG_CONFIG_HOME", "rclone/rclone.conf"},
		{"XDG_CONFIG_HOME", "sops/age/keys.txt"}, {"XDG_CONFIG_HOME", "bwrun/config.json"},
		{"XDG_DATA_HOME", "opencode/auth.json"}, {"XDG_DATA_HOME", "keyrings"},
		{"XDG_DATA_HOME", "gnome-keyring"},
	} {
		if base := xdgPaths[item.name]; base != "" {
			if err := add(filepath.Join(base, item.relative), home, "deny", -1, false); err != nil {
				return plan{}, err
			}
		}
	}
	if configDir, err := os.UserConfigDir(); err == nil {
		if err := add(filepath.Join(configDir, "bwrun", "config.json"), home, "ro", -1, false); err != nil {
			return plan{}, err
		}
	}
	if sandboxDir, found, err := findProjectSandboxDir(cwd, home); err != nil {
		return plan{}, err
	} else if found {
		resolvedDir, err := filepath.EvalSymlinks(sandboxDir)
		if err != nil {
			return plan{}, fmt.Errorf("resolve project sandbox directory %s: %w", sandboxDir, err)
		}
		entries, err := os.ReadDir(resolvedDir)
		if err != nil {
			return plan{}, fmt.Errorf("read project sandbox directory %s: %w", sandboxDir, err)
		}
		for _, entry := range entries {
			source := filepath.Join(resolvedDir, entry.Name())
			resolvedSource, err := filepath.EvalSymlinks(source)
			if err != nil {
				return plan{}, fmt.Errorf("resolve project sandbox path %s: %w", source, err)
			}
			if !within(resolvedDir, resolvedSource) {
				return plan{}, fmt.Errorf("project sandbox path %s resolves outside %s", source, resolvedDir)
			}
			target := filepath.Join(home, entry.Name())
			for path, previous := range selected {
				if within(target, path) && previous.rank < 2 {
					delete(selected, path)
				}
			}
			selected[target] = mount{path: target, source: resolvedSource, mode: "rw", rank: 2}
		}
	}
	for _, layer := range layers {
		if layer.config.Network == "deny" && layer.rank >= netRank {
			netDenied = true
			netRank = layer.rank
		} else if layer.config.Network == "allow" && layer.rank >= netRank {
			netDenied = false
			netRank = layer.rank
		}
		for _, path := range layer.config.Mounts.RO {
			if err := add(path, layer.base, "ro", layer.rank, false); err != nil {
				return plan{}, err
			}
		}
		for _, path := range layer.config.Mounts.RW {
			if err := add(path, layer.base, "rw", layer.rank, false); err != nil {
				return plan{}, err
			}
		}
		for _, path := range layer.config.Mounts.Deny {
			if err := add(path, layer.base, "deny", layer.rank, false); err != nil {
				return plan{}, err
			}
		}
	}
	for _, path := range opts.ro {
		if err := add(path, cwd, "ro", 3, true); err != nil {
			return plan{}, err
		}
	}
	for _, path := range opts.rw {
		if err := add(path, cwd, "rw", 3, true); err != nil {
			return plan{}, err
		}
	}
	for _, path := range opts.deny {
		if err := add(path, cwd, "deny", 3, false); err != nil {
			return plan{}, err
		}
	}
	// A mount over an absent child of a writable bind would require creating
	// the mount point on the host. Make only the closest containing mount
	// read-only; a closer read-only mount already prevents creation.
	for _, denied := range selected {
		if denied.mode != "deny" || !denied.missing {
			continue
		}
		closestPath := ""
		closest := mount{}
		for path, parent := range selected {
			if (parent.mode == "ro" || parent.mode == "rw") && path != denied.path && within(path, denied.path) && depth(path) > depth(closestPath) {
				closestPath, closest = path, parent
			}
		}
		if closest.mode == "rw" {
			closest.mode = "ro"
			selected[closestPath] = closest
		}
	}

	mounts := make([]mount, 0, len(selected))
	for _, item := range selected {
		if item.mode == "deny" && item.missing {
			continue
		}
		mounts = append(mounts, item)
	}
	sort.Slice(mounts, func(i, j int) bool {
		if depth(mounts[i].path) != depth(mounts[j].path) {
			return depth(mounts[i].path) < depth(mounts[j].path)
		}
		return mounts[i].path < mounts[j].path
	})

	args := []string{"--die-with-parent", "--unshare-pid", "--unshare-ipc", "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp", "--tmpfs", home}
	if opts.noNet || netDenied {
		args = append(args, "--unshare-net")
	}
	dirs := map[string]struct{}{}
	files := map[string]struct{}{}
	for _, item := range mounts {
		anchor := ""
		if within(home, item.path) {
			anchor = home
		}
		if within("/tmp", item.path) {
			anchor = "/tmp"
		}
		if anchor == "" {
			continue
		}
		for parent := filepath.Dir(item.path); within(anchor, parent) && parent != anchor; parent = filepath.Dir(parent) {
			dirs[parent] = struct{}{}
		}
		statPath := item.path
		if item.mode == "ro" || item.mode == "rw" {
			statPath = item.source
		}
		info, statErr := os.Stat(statPath)
		if item.mode == "deny" {
			if statErr == nil && info.IsDir() {
				dirs[item.path] = struct{}{}
			}
		} else if statErr == nil && info.IsDir() {
			dirs[item.path] = struct{}{}
		} else {
			files[item.path] = struct{}{}
		}
	}
	orderedDirs := make([]string, 0, len(dirs))
	for dir := range dirs {
		orderedDirs = append(orderedDirs, dir)
	}
	sort.Slice(orderedDirs, func(i, j int) bool {
		if depth(orderedDirs[i]) != depth(orderedDirs[j]) {
			return depth(orderedDirs[i]) < depth(orderedDirs[j])
		}
		return orderedDirs[i] < orderedDirs[j]
	})
	for _, dir := range orderedDirs {
		args = append(args, "--dir", dir)
	}
	orderedFiles := make([]string, 0, len(files))
	for file := range files {
		orderedFiles = append(orderedFiles, file)
	}
	sort.Strings(orderedFiles)
	for index, file := range orderedFiles {
		// Bubblewrap consumes each --file descriptor. Give every placeholder
		// a distinct fd instead of reusing one descriptor for all files.
		args = append(args, "--file", strconv.Itoa(3+index), file)
	}
	for _, item := range mounts {
		switch item.mode {
		case "ro":
			args = append(args, "--ro-bind", item.source, item.path)
		case "rw":
			args = append(args, "--bind", item.source, item.path)
		case "deny":
			info, err := os.Stat(item.path)
			if err != nil {
				continue
			}
			if info.IsDir() {
				args = append(args, "--tmpfs", item.path)
			} else {
				args = append(args, "--ro-bind", "/dev/null", item.path)
			}
		}
	}
	// Apply these last so even explicit writable rules cannot alter the policy
	// files used by a later bwrun invocation.
	for _, name := range []string{".bwrun.json", ".bwrun.local.json", ".bwrun"} {
		path := filepath.Join(cwd, name)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		} else if err != nil {
			return plan{}, fmt.Errorf("inspect runner policy path %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return plan{}, fmt.Errorf("runner policy path %s must not be a symbolic link", path)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return plan{}, fmt.Errorf("resolve runner policy path %s: %w", path, err)
		}
		args = append(args, "--ro-bind", resolved, resolved)
	}
	args = append(args, "--chdir", cwd)
	sandboxPath := filteredPath(os.Getenv("PATH"), cwd, home, mounts)
	env, err := buildEnvironment(layers, home, cwd, sandboxPath)
	if err != nil {
		return plan{}, err
	}
	args = append(args, append([]string{"--"}, command...)...)
	return plan{args: args, env: env, fileCount: len(orderedFiles)}, nil
}

// findProjectSandboxDir locates the nearest .bwrun/sandbox directory. A
// project config marks a project boundary even when it has no sandbox dir.
func findProjectSandboxDir(cwd, home string) (string, bool, error) {
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if dir != home {
			candidate := filepath.Join(dir, ".bwrun", "sandbox")
			info, err := os.Stat(candidate)
			if err == nil {
				if !info.IsDir() {
					return "", false, fmt.Errorf("project sandbox path %s is not a directory", candidate)
				}
				return candidate, true, nil
			}
			if !os.IsNotExist(err) {
				return "", false, fmt.Errorf("inspect project sandbox path %s: %w", candidate, err)
			}
			if _, err := os.Stat(filepath.Join(dir, ".bwrun.json")); err == nil {
				return "", false, nil
			} else if !os.IsNotExist(err) {
				return "", false, fmt.Errorf("inspect project config in %s: %w", dir, err)
			}
		}
		parent := filepath.Dir(dir)
		if dir == home || parent == dir {
			return "", false, nil
		}
	}
}

func filteredPath(value, cwd, home string, mounts []mount) string {
	var entries []string
	for _, entry := range filepath.SplitList(value) {
		if entry == "" {
			continue
		}
		path, err := expandPath(entry, cwd, home)
		if err != nil {
			continue
		}
		if within(home, path) {
			visible := within(cwd, path)
			for _, item := range mounts {
				if (item.mode == "ro" || item.mode == "rw") && within(item.path, path) {
					visible = true
					break
				}
			}
			if !visible {
				continue
			}
		}
		entries = append(entries, entry)
	}
	for _, entry := range []string{"/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin"} {
		if _, err := os.Stat(entry); err != nil {
			continue
		}
		found := false
		for _, existing := range entries {
			if existing == entry {
				found = true
				break
			}
		}
		if !found {
			entries = append(entries, entry)
		}
	}
	return strings.Join(entries, string(os.PathListSeparator))
}

func isDir(path string) bool { info, err := os.Stat(path); return err == nil && info.IsDir() }
func within(parent, path string) bool {
	rel, err := filepath.Rel(parent, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func depth(path string) int { return strings.Count(filepath.Clean(path), string(filepath.Separator)) }
