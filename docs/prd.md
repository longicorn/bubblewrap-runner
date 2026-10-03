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
| Config Layer: Read-Only by default                                |
|   - XDG Directories (~/.config, ~/.cache, ~/.local/share)        |
|   - Legacy Tool Paths (~/.npm, ~/.cargo, ~/.nvm, dotfiles)        |
|   - Tool Caches & Temp Runtimes: Selective RW or tmpfs            |
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
  * **Default:** Explicitly selected tool configuration and runtime paths are Read-Only (`--ro-bind`). The whole home directory is not implicitly exposed, since it may contain credentials.
  * **Cache / State Directories:** Mapped to `tmpfs` or selective Read-Write (`--bind`) when write access is essential for execution (e.g., temporary lockfiles or build cache).
  * **Built-in Catalog:** Recognizes both standard XDG directories and widespread legacy dotfiles/paths (e.g., `~/.npm`, `~/.cargo`, `~/.rustup`, `~/.gemini`, `~/.goose`, `~/.gitconfig`).

### 3.3 User Layer (User Data & Workspaces)
* **Scope:** Personal files under `$HOME` (Documents, Downloads, unrelated code repositories, sensitive credentials like `~/.ssh`, `~/.gnupg`, etc.).
* **Policy:**
  * **Default:** Deny (`hidden` / not mounted), implemented by masking the host home directory while preserving its path.
* **Current Working Directory (CWD):** Automatically mounted as Read-Write (`--bind`) so the command can perform its intended modifications within the project root.
* **Explicit Whitelists:** Paths explicitly permitted in configuration can be exposed as Read-Only or Read-Write.
* A shell started with `bwrun bash` has the same visible system paths as a host shell. Under `~/`, it sees the writable starting directory and paths explicitly allowed by policy; other host home contents remain hidden.

### 3.4 Shadow / Injection Mounts (Credential & Config Isolation)
* Secure sandbox substitution: Instead of exposing host sensitive paths (such as `~/.ssh` or environment variables), users can define isolated project-specific credentials.
* Example: If `.bwrun/sandbox/.ssh` exists in the project or is specified in config, `bwrun` mounts that custom directory onto the container's `~/.ssh`, enabling git over SSH using dedicated project keys while protecting real host SSH keys.

---

## 4. Configuration Model & Precedence

### 4.1 Configuration Hierarchy
Configuration is resolved in the following priority order (highest to lowest):
1. **Command Line Flags:** Explicit runtime arguments (e.g., `--rw <path>`, `--ro <path>`, `--deny <path>`).
2. **Project-level Configuration:** `<project-root>/.bwrun.json`.
3. **User Global Configuration:** `~/.config/bwrun/config.json`.
4. **Built-in Semantic Rules & Catalog:** Hardcoded defaults and AI-curated tool path catalog.

For mount rules, settings from a higher layer replace lower-layer rules for the same destination; nested paths are applied from parent to child. Paths in project configuration are relative to the directory containing `.bwrun.json`, global paths are relative to the user's home, and CLI paths are relative to the starting directory. `HOME` is always set to the sandbox home path. The runner starts Bubblewrap with a filtered environment: `PATH`, terminal, locale, and user identity variables are passed by default, while other variables must be listed under `env.pass` or supplied under `env.set`.

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
To relieve developers from mapping dozens of non-XDG dotfiles manually, `bwrun` includes a built-in knowledge catalog covering common tools:
* **Node.js / JavaScript:** `~/.npm`, `~/.nvm`, `~/.yarn`, `~/.pnpm-store`
* **Rust:** `~/.cargo`, `~/.rustup`
* **Python:** `~/.pyenv`, `~/.virtualenvs`, `~/.pip`
* **Go:** `~/go`, `~/.cache/go-build`
* **Git:** `~/.gitconfig`, `~/.config/git`
* **AI Agents & MCPs:** `~/.gemini`, `~/.goose`, `~/.claude`, global skills/recipes directories.

---

## 5. CLI Interface & User Experience (UX)

### 5.1 Command Name
* **Command:** `bwrun` (with optional full alias `bubblewrap-runner`).

### 5.2 Usage Syntax
```bash
bwrun [flags] -- <command> [args...]
# A command may also follow directly, for example: bwrun bash
```

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
