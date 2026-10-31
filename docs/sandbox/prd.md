# Product Requirements Document (PRD): Sandbox & Semantic Layering (`/sandbox`)

This document defines the detailed filesystem isolation, semantic layering, workspace boundary validation, project sandbox overlays, and built-in tool catalog specifications for `bwrun` (bubblewrap-runner).

- **Product Overview, CLI & Roadmap:** [../prd.md](../prd.md)
- **Configuration & Environment Specifications:** [../config/prd.md](../config/prd.md)
- **Sandbox & Isolation ADRs:** [adr.md](adr.md)

---

## 1. Architecture & Semantic Layering Model

The core concept of `bwrun` is partitioning the host filesystem into distinct functional layers and applying a tailored policy to each layer:

```
+-------------------------------------------------------------------+
| User Layer: Home masked, with defined exceptions                 |
|   - Current Working Directory (CWD): READ-WRITE (RW)              |
|   - Project sandbox entries RW; explicit paths RO / RW / Deny     |
+-------------------------------------------------------------------+
| Config Layer: Dotfiles RO; XDG and known caches RW                |
|   - XDG config/cache/data/state directories                       |
|   - Legacy runtimes RO; package caches RW                         |
|   - Known credentials and histories denied                       |
+-------------------------------------------------------------------+
| Base Layer: Host root visible read-only                           |
|   - /, including system paths and /home                            |
+-------------------------------------------------------------------+
```

### 1.1 Base Layer (System Infrastructure)
* **Scope:** The host root filesystem (`/`) is initially visible at the same paths inside the sandbox, including `/usr`, `/lib`, `/bin`, `/etc`, `/var`, and `/home`. The host home directory is then masked and selectively repopulated as described below.
* **Policy:**
  * The root filesystem is read-only by default. `/dev` and `/proc` are provided as sandbox instances. This makes commands such as `ls /` and host-installed tools behave as expected while keeping writes outside permitted paths from changing host files.
  * **Namespace Isolation:** Bubblewrap creates unshared PID (`--unshare-pid`) and IPC (`--unshare-ipc`) namespaces by default. Sandboxed processes cannot inspect, signal, or communicate via shared memory/semaphores with host processes outside the sandbox. A fresh `/proc` instance is mounted to strictly reflect sandboxed processes.

### 1.2 Config Layer (Developer Tools, Dotfiles, & Runtimes)
* **Scope:** Tool configuration directories, runtime caches, and shared assets (e.g., prompt templates, agent skills, shared memo directories).
* **Policy:**
  * Existing top-level dotfiles and dotdirectories are mounted Read-Only by default.
  * XDG config, cache, data, and state directories are mounted Read-Write. Custom `XDG_CONFIG_HOME`, `XDG_CACHE_HOME`, `XDG_DATA_HOME`, and `XDG_STATE_HOME` values are honored when their paths exist.
  * A built-in catalog assigns write access to known package caches and state paths, including `~/.npm`, `~/.pnpm-store`, `~/.yarn`, Cargo registry/git caches, Go module caches, `~/.gem`, `~/.gradle`, and `~/.m2/repository`. Toolchains and shared runtimes such as `~/.nvm`, `~/.rustup`, `~/.pyenv`, and `~/go` remain Read-Only.
  * Known credentials, private keys, shell histories, and autostart/persistence vectors (specifically `~/.config/autostart` and `~/.config/systemd/user`) are denied even when their parent dotdirectory is mounted. Explicit project, user, or CLI rules can override the built-in catalog.

### 1.3 User Layer (User Data & Workspaces)
* **Scope:** Personal files under `$HOME` (Documents, Downloads, unrelated code repositories, sensitive credentials like `~/.ssh`, `~/.gnupg`, etc.).
* **Policy:**
  * **Default:** Mask the host home directory while preserving its path. Existing top-level dotfiles, XDG directories, and known tool caches are then exposed according to the Config Layer rules in Section 1.2; other home contents remain hidden unless a workspace, project sandbox entry, or explicit rule exposes them.
* **Current Working Directory (CWD) & Workspace Boundary Validation:** Automatically mounted as Read-Write (`--bind`) so the command can perform its intended modifications within the workspace, subject to strict boundary guards that prevent accidental exposure of `$HOME` or broad parent directories (such as `~/src`):
  * **Protected Non-Workspace Paths:** Starting `bwrun` in `$HOME`, `/`, or any ancestor directory containing `$HOME` (such as `/home`) is unconditionally rejected, even when `--allow-cwd` is passed.
  * **Upward Project Marker Discovery:** Before mounting CWD Read-Write by default, `bwrun` traverses upward from the starting CWD toward its parent directories—stopping before `$HOME` and `/` without inspecting `$HOME` itself—to verify that CWD lies within a project boundary. By default, `bwrun` looks for `.git` (either a directory or a file, supporting git worktrees and submodules); `.bwrun.json` is also always recognized as a valid project boundary. Custom marker names can be configured via the top-level `"workspace-markers"` configuration key (e.g., `[".git", ".hg", ".jj"]`).
  * **Scoped CWD Mount:** When a marker is found in CWD or an allowed ancestor directory, only the starting CWD itself (not the ancestor directory where the marker was found) is mounted Read-Write (`--bind`).
  * **Explicit Non-Project Override (`--allow-cwd`):** If no project marker is found before reaching `$HOME` or `/`, `bwrun` refuses to start by default and instructs the user to change to a project directory, run `bwrun init`, or pass `--allow-cwd`. Passing `--allow-cwd` explicitly authorizes mounting the current directory Read-Write when working outside a tracked project (while still rejecting `/`, `$HOME`, and ancestors of `$HOME`).
  * **Runner Self-Protection:** Any configuration files or directories controlling sandbox behavior inside CWD (specifically `.bwrun.json`, `.bwrun.local.json`, and the `.bwrun/` directory) are automatically re-mounted as Read-Only (`--ro-bind`) over the writable CWD. This prevents sandboxed processes from tampering with security policies for subsequent runs.
* **Explicit Whitelists:** Paths explicitly permitted in configuration can be exposed as Read-Only or Read-Write.
* A shell started with `bwrun bash` has the same visible system paths as a host shell. Under `~/`, it sees the writable starting directory, the Config Layer's automatic mounts, project sandbox entries, and paths explicitly allowed by policy; other host home contents remain hidden.

### 1.4 Project Sandbox Overlays (Credential & Config Isolation)
* **Secure sandbox substitution:** Instead of exposing host sensitive paths (such as `~/.ssh` or environment variables), users can define isolated project-specific credentials and configurations.
* **Default project overlay:** Entries directly inside the nearest project `.bwrun/sandbox/` directory are automatically mounted Read-Write into the sandbox home at the same relative paths. For example, `.bwrun/sandbox/.ssh` is mounted at `~/.ssh`, enabling git over SSH with project-specific keys while keeping host keys hidden. Explicit project or CLI mount rules can override the automatic mapping.
* **Explicit overlay selection:** `--sandbox-dir <dir>` or the top-level `"sandbox-dir"` configuration key selects a directory instead of automatic project discovery, including when `--config` is used. Direct child files and directories are mounted Read-Write at their home-relative paths; a directory replaces the corresponding host directory rather than merging its contents. The selected directory must exist and be a directory. It must not contain the file selected by `--config`, preventing that policy file from becoming writable through a home alias. CLI selection takes precedence over configuration; project configuration takes precedence over global configuration. Relative `sandbox-dir` paths are always resolved from the starting CWD, both on the CLI and in all configuration layers, including files selected with `--config`. `~` and host environment variables are expanded. Explicit project/profile or CLI mount rules can override overlay entries. If no directory is specified, the existing discovery behavior is retained.
* **Explicit nested bindings:** `mounts.rw` also accepts `{"source": "<host path>", "target": "<sandbox path>"}` objects alongside string entries. A parent host directory can remain shared while a child is replaced by a project-local directory. For example, expose `~/.copilot` with a string rule and bind `.bwrun/sandbox/.copilot/session-state` at `~/.copilot/session-state` with an object rule. Host session history stays hidden at that path, and sandbox sessions persist in the source directory across invocations. This works with `--config` and does not require `sandbox-dir`. Both relative object paths are resolved from the starting CWD, with `~` and host environment expansion. The source must exist; the target may be absent. Root and home paths cannot be bound wholesale, sources cannot contain protected runner policy files, and existing targets must match the source's file/directory type and cannot be symbolic links. Bindings follow the normal layer precedence by destination and mount parents before children regardless of array order. `mounts.deny` and CLI denials can mask mapped destinations, including those absent on the host.

---

## 2. Built-in Tool Catalog

`bwrun` comes with a built-in catalog of ecosystem and runtime paths. This catalog is authored and curated with the assistance of AI analysis across various language and agent ecosystems, and embedded into the runner as a declarative data structure.

To ensure long-term maintainability without burdening developers:
* **Declarative Separation:** Catalog rules are maintained as declarative definitions (e.g. embedded JSON/data structs) separate from execution logic, keeping updates frictionless as tool paths change.
* **Extensibility & Overrides:** While standard runtimes are embedded for single-binary zero-configuration execution, Phase 2 introduces support for loading user-defined catalog additions (e.g., from `~/.config/bwrun/catalog.d/`) so users can define rules for proprietary or emerging tools without recompiling the runner.

To relieve developers from mapping dozens of non-XDG paths manually, `bwrun` discovers existing top-level dotfiles and dotdirectories and mounts them read-only, then applies this built-in catalog for common caches and state:
* **Node.js / JavaScript:** `~/.npm`, `~/.pnpm-store`, `~/.yarn` writable; `~/.nvm` and tool configuration read-only.
* **Rust:** `~/.cargo` read-only except `registry` and `git` caches; `~/.rustup` read-only.
* **Python:** runtime/configuration paths such as `~/.pyenv`, `~/.virtualenvs`, and `~/.pip` read-only; XDG caches writable.
* **Go:** `~/go` read-only except module and checksum databases; XDG build cache writable.
* **Java / Ruby:** Gradle and Maven repositories plus RubyGems caches writable; credential-bearing settings files denied.
* **AI agents and developer tools:** dotdirectories such as `~/.gemini`, `~/.goose`, and `~/.claude` read-only, with known credential files denied.

The catalog also denies common secret paths such as `~/.ssh`, `~/.gnupg`, `~/.aws`, `~/.azure`, `~/.kube`, `~/.codex/auth.json`, agent login files, `~/.netrc`, package registry credentials, shell histories, and autostart/persistence vectors (`~/.config/autostart`, `~/.config/systemd/user`). Higher-priority explicit rules can allow a path when a project intentionally needs it.
