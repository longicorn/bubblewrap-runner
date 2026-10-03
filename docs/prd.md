# Product Requirements Document (PRD): bwrun (bubblewrap-runner)

## 1. Overview & Problem Statement

### 1.1 Background
With the rise of autonomous and semi-autonomous AI coding agents (such as Goose, Claude Code, Gemini CLI, etc.), securing agent execution while preserving developer productivity has become a critical challenge.
* **Containers (e.g., Docker) are too heavy and cumbersome:** Setting up isolated containers for dozens of personal projects requires extensive mount configurations (sharing shared developer tools, global skills, dotfiles, memo files, caches, runtime environments, etc.). Maintaining container environments across diverse development projects creates excessive administrative friction.
* **Direct host execution is dangerous:** In practice, developers frequently run AI agents directly on their host machines and grant tool-based file modification privileges (e.g., via Computer Controller MCPs or bash tools). Even though developers understand the risk of accidental host-wide destruction or exfiltration of sensitive files (`~/.ssh`, `~/.aws`, personal documents), the overhead of containerization makes it difficult to adopt containers for daily personal development.
* **Bubblewrap (`bwrap`) is powerful but complex:** Bubblewrap provides lightweight, unprivileged user-namespace sandboxing on Linux without container runtime overhead. However, handcrafting bwrap command-line arguments (mounting dozens of system paths, selective home directory mappings, handling permissions) for each project and tool is tedious and error-prone.

### 1.2 Proposed Solution
`bwrun` (bubblewrap-runner) is an automated, intelligent sandbox runner built on top of `bubblewrap`. It analyzes the semantics of the filesystem and tool ecosystems, dynamically builds fine-grained bubblewrap arguments, and executes commands safely and transparently with zero-to-low configuration.

---

## 2. Product Vision & Goals

### 2.1 Vision
Enable developers to run any AI agent or untrusted CLI tool as effortlessly as running it on the host, with rock-solid security boundaries automatically established by semantic awareness.

### 2.2 Goals
1. **Zero-Configuration Usability:** Running `bwrun -- <cmd>` works out of the box for standard development workflows without manual sandbox configuration.
2. **Semantic Layering:** Automatically classify paths into Base, Config, and User layers with secure-by-default access policies.
3. **Smart Ecosystem Catalog:** Pre-package knowledge of legacy and non-XDG paths (e.g., `~/.npm`, `~/.cargo`, `~/.nvm`, `~/.gitconfig`, AI agent directories) so developers don't have to manually locate and configure tool files.
4. **Hierarchical Configuration & Shadow Injection:** Allow project-level and user-level overrides, including shadow mounts (e.g., mapping project-specific isolated credentials over `~/.ssh`).
5. **High Performance & Portability:** Single static Go binary with negligible startup latency.

### 2.3 Non-Goals
* Re-implementing a container daemon or image management system (Docker/OCI alternative).
* Non-Linux OS support in the initial phase (bubblewrap relies on Linux user namespaces).
* Hypervisor-level virtualization or kernel-level anomaly detection.

---

## 3. Architecture & Semantic Layering Model

The core concept of `bwrun` is partitioning the host filesystem into distinct functional layers and applying a tailored policy to each layer:

```
+-------------------------------------------------------------------+
| User Layer: Deny by default (Hidden)                              |
|   - Current Working Directory (CWD): READ-WRITE (RW)              |
|   - Specific Whitelisted Paths: RO / RW / Deny                    |
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

### 3.1 Base Layer (System Infrastructure)
* **Scope:** The host root filesystem (`/`) is visible at the same paths inside the sandbox, including `/usr`, `/lib`, `/bin`, `/etc`, `/var`, and `/home`.
* **Policy:** The root filesystem is read-only by default. `/dev` and `/proc` are provided as sandbox instances. This makes commands such as `ls /` and host-installed tools behave as expected while keeping writes outside permitted paths from changing host files.

### 3.2 Config Layer (Developer Tools, Dotfiles, & Runtimes)
* **Scope:** Tool configuration directories, runtime caches, and shared assets (e.g., prompt templates, agent skills, shared memo directories).
* **Policy:**
  * Existing top-level dotfiles and dotdirectories are mounted Read-Only by default.
  * XDG config, cache, data, and state directories are mounted Read-Write. Custom `XDG_CONFIG_HOME`, `XDG_CACHE_HOME`, `XDG_DATA_HOME`, and `XDG_STATE_HOME` values are honored when their paths exist.
  * A built-in catalog assigns write access to known package caches and state paths, including `~/.npm`, `~/.pnpm-store`, `~/.yarn`, Cargo registry/git caches, Go module caches, `~/.gem`, `~/.gradle`, and `~/.m2/repository`. Toolchains and shared runtimes such as `~/.nvm`, `~/.rustup`, `~/.pyenv`, and `~/go` remain Read-Only.
  * Known credentials, private keys, and shell histories are denied even when their parent dotdirectory is mounted. Explicit project, user, or CLI rules can override the built-in catalog.

### 3.3 User Layer (User Data & Workspaces)
* **Scope:** Personal files under `$HOME` (Documents, Downloads, unrelated code repositories, sensitive credentials like `~/.ssh`, `~/.gnupg`, etc.).
* **Policy:**
  * **Default:** Deny (`hidden` / not mounted), implemented by masking the host home directory while preserving its path.
* **Current Working Directory (CWD):** Automatically mounted as Read-Write (`--bind`) so the command can perform its intended modifications within the project root.
* **Explicit Whitelists:** Paths explicitly permitted in configuration can be exposed as Read-Only or Read-Write.
* A shell started with `bwrun bash` has the same visible system paths as a host shell. Under `~/`, it sees the writable starting directory and paths explicitly allowed by policy; other host home contents remain hidden.

### 3.4 Shadow / Injection Mounts (Credential & Config Isolation)
* Secure sandbox substitution: Instead of exposing host sensitive paths (such as `~/.ssh` or environment variables), users can define isolated project-specific credentials.
* Default project overlay: entries directly inside the nearest project `.bwrun/sandbox/` directory are automatically mounted Read-Write into the sandbox home at the same relative paths. For example, `.bwrun/sandbox/.ssh` is mounted at `~/.ssh`, enabling git over SSH with project-specific keys while keeping host keys hidden. Explicit project or CLI mount rules can override the automatic mapping.

---

## 4. Configuration Model & Precedence

### 4.1 Configuration Hierarchy
Configuration is resolved in the following priority order (highest to lowest):
1. **Command Line Flags:** Explicit runtime arguments (e.g., `--rw <path>`, `--ro <path>`, `--deny <path>`).
2. **Project-level Configuration:** `<project-root>/.bwrun.json`.
3. **User Global Configuration:** `~/.config/bwrun/config.json`.
4. **Built-in Semantic Rules & Catalog:** Hardcoded defaults and AI-curated tool path catalog.

For mount rules, settings from a higher layer replace lower-layer rules for the same destination; nested paths are applied from parent to child. Paths in project configuration are relative to the directory containing `.bwrun.json`, global paths are relative to the user's home, and CLI paths are relative to the starting directory. `HOME` is always set to the sandbox home path, and `BWRUN_SANDBOX=1` is always set so shells and scripts can identify sandbox execution. Existing host environment variables are passed through by default; `env.deny` filters variables and `env.set` sets or overrides them. `env.pass` can explicitly request a variable that is otherwise absent from the host environment when available.

### 4.2 Configuration Schema (JSON Example)
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "version": "1",
  "network": "allow",
  "mounts": {
    "ro": [
      "~/.config/goose/recipes",
      "~/.local/share/agent-skills"
    ],
    "rw": [
      "~/.cache/my-tool"
    ],
    "deny": [
      "~/.aws",
      "~/.config/gcloud"
    ]
  },
  "shadow": [
    {
      "target": "~/.ssh",
      "source": ".bwrun/sandbox/ssh"
    }
  ],
  "env": {
    "pass": [
      "PATH",
      "TERM",
      "LANG"
    ],
    "set": {
      "CI": "true"
    }
  }
}
```

### 4.3 AI-Curated Tool Catalog
To relieve developers from mapping dozens of non-XDG paths manually, `bwrun` discovers existing top-level dotfiles and dotdirectories and mounts them read-only, then applies a built-in writable-path catalog for common caches and state:
* **Node.js / JavaScript:** `~/.npm`, `~/.pnpm-store`, `~/.yarn` writable; `~/.nvm` and tool configuration read-only.
* **Rust:** `~/.cargo` read-only except `registry` and `git` caches; `~/.rustup` read-only.
* **Python:** runtime/configuration paths such as `~/.pyenv`, `~/.virtualenvs`, and `~/.pip` read-only; XDG caches writable.
* **Go:** `~/go` read-only except module and checksum databases; XDG build cache writable.
* **Java / Ruby:** Gradle and Maven repositories plus RubyGems caches writable; credential-bearing settings files denied.
* **AI agents and developer tools:** dotdirectories such as `~/.gemini`, `~/.goose`, and `~/.claude` read-only, with known credential files denied.

The catalog also denies common secret paths such as `~/.ssh`, `~/.gnupg`, `~/.aws`, `~/.azure`, `~/.kube`, `~/.codex/auth.json`, agent login files, `~/.netrc`, package registry credentials, and shell histories. Higher-priority explicit rules can allow a path when a project intentionally needs it.

---

## 5. CLI Interface & User Experience (UX)

### 5.1 Command Name
* **Command:** `bwrun`.

### 5.2 Usage Syntax
```bash
bwrun init
bwrun [flags] -- <command> [args...]
# A command may also follow directly, for example: bwrun bash
```

`bwrun init` creates a starter `.bwrun.json` in the current directory and never overwrites an existing file. The template starts with empty path and environment rules; developers add only the permissions their project needs.

#### Examples:
```bash
# Run Goose agent safely in the current project
bwrun -- goose

# Run Gemini CLI with extra read-only path
bwrun --ro ~/docs/shared -- gemini

# Run interactive bash inside the sandboxed environment
bwrun -- bash

# Dry-run to inspect the generated bwrap command line
bwrun --dry-run -- goose
```

### 5.3 Execution & Process Management
* **Transparent Pass-through:** Standard input, output, and error are streamed transparently.
* **Exit Code & Signals:** `bwrun` proxies all Linux signals (SIGINT, SIGTERM, SIGHUP) to the child process and returns the child's exact exit code.
* **Network Model:** Enabled by default (sharing the host network namespace to support LLM API calls, package downloads, and web search), with an option (`--no-net`) for completely isolated execution.

---

## 6. Technical Foundation

* **Programming Language:** Go (Golang)
  * Fast execution, minimal memory overhead, zero runtime dependencies.
  * Easy single-binary distribution (via GitHub Releases, Homebrew, Arch AUR, etc.).
  * Native system calls and process execution control in Linux environments.
* **External Runtime Dependency:** `bwrap` (bubblewrap) executable installed on the host system.
* **Target Operating System:** Linux (Kernel with unprivileged user namespaces enabled).

---

## 7. Roadmap & Phased Implementation

### Phase 1: MVP (Core Runner)
* CLI with `bwrun [flags] -- <command>` and the convenient `bwrun <command>` form.
* `bwrun init` to create a project configuration template without overwriting existing settings.
* Automatic generation of Base Layer, Config Layer, and User Layer. The host `/` is visible read-only, the starting directory is RW, and other home paths are hidden unless explicitly allowed.
* Support for project config (`.bwrun.json`) and user global config (`~/.config/bwrun/config.json`).
* `--dry-run` flag to inspect generated `bwrap` invocation.

### Phase 2: Built-in Catalog & Shadow Mounts
* Comprehensive built-in catalog for standard runtimes (Node, Python, Go, Rust) and AI CLI tools.
* Shadow mount support (substituting paths like `~/.ssh`).
* Interactive/TUI diagnostic command (`bwrun doctor`) to verify bwrap availability and active mount policies.

### Phase 3: Advanced Controls & Hooks
* Fine-grained network filtering/proxy integration.
* Pre-exec and post-exec lifecycle hooks.
