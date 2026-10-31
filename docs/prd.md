# Product Requirements Document (PRD): bwrun (bubblewrap-runner)

- **Sandbox & Semantic Layering Specifications (`/sandbox`):** [sandbox/prd.md](sandbox/prd.md)
- **Configuration & Environment Specifications (`/config`):** [config/prd.md](config/prd.md)
- **Architecture Decision Records (ADRs):** [adr.md](adr.md), [sandbox/adr.md](sandbox/adr.md), [config/adr.md](config/adr.md)
- **Troubleshooting Guide:** [troubleshooting.md](troubleshooting.md)

---

## 1. Overview & Problem Statement

### 1.1 Background
With the rise of autonomous and semi-autonomous AI coding agents (such as Goose, Claude Code, Gemini CLI, etc.), securing agent execution while preserving developer productivity has become a critical challenge.
* **Containers (e.g., Docker) are too heavy and cumbersome:** Setting up isolated containers for dozens of personal projects requires extensive mount configurations (sharing shared developer tools, global skills, dotfiles, memo files, caches, runtime environments, etc.). Maintaining container environments across diverse development projects creates excessive administrative friction.
* **Direct host execution is dangerous:** In practice, developers frequently run AI agents directly on their host machines and grant tool-based file modification privileges (e.g., via Computer Controller MCPs or bash tools). Even though developers understand the risk of accidental host-wide destruction or exfiltration of sensitive files (`~/.ssh`, `~/.aws`, personal documents), the overhead of containerization makes it difficult to adopt containers for daily personal development.
* **Bubblewrap (`bwrap`) is powerful but complex:** Bubblewrap provides lightweight, unprivileged user-namespace sandboxing on Linux without container runtime overhead. However, handcrafting bwrap command-line arguments (mounting dozens of system paths, selective home directory mappings, handling permissions) for each project and tool is tedious and error-prone.

### 1.2 Proposed Solution & Practical Threat Model
`bwrun` (bubblewrap-runner) is an automated, intelligent sandbox runner built on top of `bubblewrap`. It analyzes the semantics of the filesystem and tool ecosystems, dynamically builds fine-grained bubblewrap arguments, and executes commands safely and transparently with zero-to-low configuration.

#### Practical Threat Model & Realistic Security Goals
`bwrun` does not claim to prevent sophisticated kernel-level zero-day sandbox escapes or host takeovers if an AI agent runs dedicated adversarial exploit payloads. Instead, it addresses the **primary practical risks of daily host execution**:
1. **Cross-Project Code & Data Leakage:** Developers often participate in multiple internal or proprietary repositories. An autonomous agent running in Project A could inadvertently (or maliciously) read, index, or exfiltrate source code from Project B under `$HOME/src/...`.
2. **Credential Theft & Accidental Host Damage:** Unintended reading or modification of sensitive keys (`~/.ssh`, `~/.aws`, `~/.gnupg`, browser cookies, API tokens, shell histories).
3. **Persistence Vector Planting:** Placing autostart scripts (`~/.config/autostart`, systemd user units) to gain persistence on user login.

`bwrun` delivers a massive security upgrade over direct host execution without incurring container virtualization overhead.

---

## 2. Product Vision & Goals

### 2.1 Vision
Enable developers to run any AI agent or untrusted CLI tool as effortlessly as running it on the host, with rock-solid practical security boundaries automatically established by semantic awareness.

### 2.2 Goals
1. **Zero-Configuration Usability:** Running `bwrun -- <cmd>` works out of the box for standard development workflows without manual sandbox configuration.
2. **Semantic Layering & Scoped Visibility:** Automatically classify paths into Base, Config, and User layers. The host root (`/`) is visible read-only to ensure host toolchains function seamlessly, while the host `$HOME` is masked with strict, fine-grained access policies.
3. **Built-in Tool Catalog:** Pre-package knowledge of legacy and non-XDG paths (e.g., `~/.npm`, `~/.cargo`, `~/.nvm`, `~/.gitconfig`, AI agent directories) so developers don't have to manually locate and configure tool files.
4. **Hierarchical Configuration & Project Sandbox Overlays:** Allow project-level and user-level overrides, including project sandbox overlays (e.g., automatically mapping project-specific isolated credentials under `.bwrun/sandbox/` over `~/.ssh`).
5. **High Performance & Portability:** Single static Go binary with negligible startup latency.

### 2.3 Non-Goals
* Hypervisor-level virtualization or kernel-level anomaly detection (does not defend against kernel privilege-escalation exploits).
* Re-implementing a container daemon or image management system (Docker/OCI alternative).
* Complete isolation of host system root binaries (host `/usr`, `/bin`, `/lib`, and `/etc` are intentionally shared read-only for frictionless runtime reuse).
* Non-Linux OS support in the initial phase (bubblewrap relies on Linux user namespaces).

---

## 3. Architecture & Semantic Layering Summary

`bwrun` partitions the host filesystem into three functional layers and applies a tailored isolation policy to each:

1. **Base Layer (System Infrastructure):** The host root filesystem (`/`) is visible read-only; `/dev`, `/proc`, and `/tmp` are instantiated for the sandbox, and PID (`--unshare-pid`) and IPC (`--unshare-ipc`) namespaces are unshared by default.
2. **Config Layer (Developer Tools, Dotfiles, & Runtimes):** Existing top-level dotfiles and dotdirectories are mounted read-only; XDG directories and cataloged package caches are mounted read-write; known credentials, shell histories, and autostart/persistence vectors (`~/.config/autostart`, `~/.config/systemd/user`) are denied.
3. **User Layer & Workspace Boundaries:** The host `$HOME` is masked at its path. The starting Current Working Directory (CWD) is mounted read-write only after passing workspace boundary validation (upward project marker discovery via `"workspace-markers"` or `.bwrun.json`, or explicit `--allow-cwd`, while unconditionally rejecting `/`, `$HOME`, and ancestors of `$HOME`). Runner policy files inside CWD (`.bwrun.json`, `.bwrun.local.json`, `.bwrun/`) are re-mounted read-only for self-protection.
4. **Project Sandbox Overlays & Built-in Tool Catalog:** Project-local overlays (`.bwrun/sandbox/`, `--sandbox-dir`, `"sandbox-dir"`, and `mounts.rw` `{source, target}` nested bindings) isolate sensitive paths and session state per project or profile, backed by a declarative built-in tool catalog.

For the complete normative specifications of semantic layering, workspace boundary validation, project sandbox overlays, and the built-in tool catalog, see [docs/sandbox/prd.md](sandbox/prd.md) and [docs/sandbox/adr.md](sandbox/adr.md).

---

## 4. Configuration & Environment Summary

`bwrun` resolves configuration and environment variables across a four-tier hierarchy (highest to lowest):

1. **Command Line Flags** (`--rw`, `--ro`, `--deny`, `--sandbox-dir`, `--allow-cwd`, `--no-net`, etc.)
2. **Project-level Configuration** (`<project-root>/.bwrun.json`)
3. **User Global Configuration** (`~/.config/bwrun/config.json`)
4. **Built-in Semantic Rules & Catalog**

Passing `--config <file>` selects an explicit configuration file in place of automatic global/project config and overlay discovery. Mount rules follow destination precedence and parent-to-child ordering with layer-specific relative path resolution rules. Host environment variables are inherited by default, filtered by `env.deny`, overridden by `env.set`, and supplemented by `env.pass`, with `HOME` and `BWRUN_SANDBOX=1` enforced inside the sandbox.

For the complete normative specifications of configuration precedence, `--config` behavior, relative path resolution, `.bwrun.json.sample` schema, and environment variable controls, see [docs/config/prd.md](config/prd.md) and [docs/config/adr.md](config/adr.md).

---

## 5. CLI Interface & User Experience (UX)

### 5.1 Command Name
* **Command:** `bwrun`.

### 5.2 Usage Syntax
```bash
bwrun init
bwrun [flags] [--] <command> [args...]
```

Use `--` before the command if it takes option flags (starting with `-`), ensuring they are not parsed as `bwrun` options. Commands without flags (for example, `bwrun bash`) can be specified directly without `--`.

`bwrun init` creates a starter `.bwrun.json` in the current directory and never overwrites an existing file. The template starts with empty path and environment rules; developers add only the permissions their project needs.

When starting outside a directory tree marked by `.git` (or a configured `"workspace-markers"` entry) or `.bwrun.json`, pass `--allow-cwd` to explicitly permit mounting the starting directory Read-Write (`/` and `$HOME` or its ancestors remain forbidden).

#### Examples:
```bash
# Run Goose agent safely in the current project
bwrun goose
# Or with flags for the target command
bwrun -- goose --version

# Run Gemini CLI with extra read-only path
bwrun --ro ~/docs/shared -- gemini

# Run interactive bash inside the sandboxed environment
bwrun bash

# Explicitly allow mounting CWD when running in a non-git scratch directory
bwrun --allow-cwd -- bash

# Dry-run to inspect the generated bwrap command line
bwrun --dry-run -- goose
bwrun --config ~/.config/bwrun/profiles/agent.json -- goose
bwrun --config ~/.config/bwrun/profiles/agent.json --sandbox-dir .bwrun/sandbox -- goose
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
**Status: Complete (2026-10-04).** The scope below is implemented. Unit tests and a real-`bwrap` integration test cover the core isolation boundary; further improvements can continue in later phases before a v1 release.

* CLI with `bwrun [flags] -- <command>` and the convenient `bwrun <command>` form.
* `bwrun init` to create a project configuration template without overwriting existing settings.
* Automatic generation of Base Layer, Config Layer, and User Layer. The host `/` is visible read-only, the starting directory is RW, and home paths are hidden except for automatic Config Layer mounts, project sandbox entries, and explicitly allowed paths.
* Support for project config (`.bwrun.json`) and user global config (`~/.config/bwrun/config.json`).
* `--dry-run` flag to inspect generated `bwrap` invocation.
* Actionable child-process execution error intercepting: Detect unprivileged user namespace restrictions (e.g., AppArmor permissions on Ubuntu 24.04+) and output clear resolution hints pointing to `docs/troubleshooting.md`.

### Phase 2: Data-Driven Catalog, Practical Isolation & Diagnostics
* Data-Driven Tool Catalog: Modular catalog definition with embedded JSON specifications and external user overrides (`~/.config/bwrun/catalog.d/*.json`).
* Clean Environment Mode (`--clean-env` / `"inherit": false`): Run with an isolated host environment, selectively passing only explicit variables (`env.pass`) to prevent accidental host secret exfiltration.
* External Environment File Support (`env_file`): Populate environment variables from specified files (e.g., `.env`) without hardcoding them in configuration.
* Project Sandbox Overlays: Automatically map project-specific isolated credentials under `.bwrun/sandbox/` over home directories (e.g. `~/.ssh`).
* Diagnostic and health-check command (`bwrun doctor`):
  * Verify `bwrap` availability, version, and unprivileged user namespace permissions.
  * Scan `$HOME` for uncataloged dotpaths to detect untracked sensitive assets and guide additions to `catalog.d/`.
  * Validate active mount policies and project configurations.

### Phase 3: Advanced Controls & Ecosystem Integration
* Fine-grained network filtering and transparent proxy integration.
* Pre-exec and post-exec lifecycle hooks.
* Execution audit logging and structured event tracing.
* Community catalog sharing and profile repositories.
