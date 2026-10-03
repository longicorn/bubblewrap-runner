package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type mount struct {
	path   string
	source string
	mode   string
	rank   int
}

type plan struct {
	args []string
	env  []string
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
		info, statErr := os.Stat(path)
		if statErr != nil {
			if os.IsNotExist(statErr) && !requireExisting && mode == "deny" {
				return nil
			}
			if os.IsNotExist(statErr) && !requireExisting {
				return nil
			}
			return fmt.Errorf("path %s: %w", path, statErr)
		}
		if mode == "ro" || mode == "rw" {
			resolved, err = filepath.EvalSymlinks(path)
			if err != nil {
				return fmt.Errorf("resolve path %s: %w", path, err)
			}
			if info.IsDir() != isDir(resolved) {
				return fmt.Errorf("invalid mount source %s", path)
			}
		}
		candidate := mount{path: path, source: resolved, mode: mode, rank: rank}
		if old, ok := selected[path]; !ok || rank >= old.rank {
			selected[path] = candidate
		}
		return nil
	}

	// Default workspace access has the lowest precedence and may be narrowed by policy.
	if err := add(cwd, cwd, "rw", 0, true); err != nil {
		return plan{}, err
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

	mounts := make([]mount, 0, len(selected))
	for _, item := range selected {
		mounts = append(mounts, item)
	}
	sort.Slice(mounts, func(i, j int) bool {
		if depth(mounts[i].path) != depth(mounts[j].path) {
			return depth(mounts[i].path) < depth(mounts[j].path)
		}
		return mounts[i].path < mounts[j].path
	})

	args := []string{"--die-with-parent", "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp", "--tmpfs", home}
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
		info, statErr := os.Stat(item.path)
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
	for _, file := range orderedFiles {
		args = append(args, "--file", "3", file)
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
	args = append(args, "--chdir", cwd)
	sandboxPath := filteredPath(os.Getenv("PATH"), cwd, home, mounts)
	env, err := buildEnvironment(layers, home, cwd, sandboxPath)
	if err != nil {
		return plan{}, err
	}
	args = append(args, append([]string{"--"}, command...)...)
	return plan{args: args, env: env}, nil
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
