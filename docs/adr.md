# Architecture Decision Records (ADR) - bwrun (bubblewrap-runner)

This document records the architectural and design decisions made for `bwrun` (bubblewrap-runner).

---

## Table of Contents

- [ADR-0001: Bubblewrap (bwrap) as the Core Isolation Engine](#adr-0001-bubblewrap-bwrap-as-the-core-isolation-engine)
- [ADR-0002: Go (Golang) as the Implementation Language](#adr-0002-go-golang-as-the-implementation-language)
- [ADR-0003: Three-Tier Semantic Layering Model](#adr-0003-three-tier-semantic-layering-model)
- [ADR-0004: JSON as the Primary Configuration Format](#adr-0004-json-as-the-primary-configuration-format)
- [ADR-0005: Runtime Existence-Checking for Dynamic Mount Construction](#adr-0005-runtime-existence-checking-for-dynamic-mount-construction)
- [ADR-0006: Child Process Execution with Transparent I/O and Signal Forwarding](#adr-0006-child-process-execution-with-transparent-io-and-signal-forwarding)
- [ADR-0007: Default Host Network Passthrough with Opt-Out Flag](#adr-0007-default-host-network-passthrough-with-opt-out-flag)
- [ADR-0008: Default Environment Variable Pass-Through with Explicit Deny/Override](#adr-0008-default-environment-variable-pass-through-with-explicit-denyoverride)

---

## ADR-0001: Bubblewrap (bwrap) as the Core Isolation Engine

### Status
Accepted

### Context
Developers executing AI coding agents (Goose, Gemini CLI, Claude Code, etc.) or untrusted developer tools on host machines face significant risks of accidental host damage or credential leakage (e.g., `~/.ssh`, `~/.aws`). 
Existing containerization tools like Docker or Podman introduce substantial operational friction:
- Heavy startup latency.
- Complicated host tool/runtime sharing (Node, Python, Go, Rust, system packages).
- Tedious mounting of dotfiles, agent skills, and prompt templates.
Direct execution on the host is frictionless but unsafe. We need an unprivileged, low-overhead isolation mechanism on Linux that can selectively expose host resources.

### Decision
Use `bubblewrap` (`bwrap`) as the underlying sandboxing engine. `bwrun` will serve as an intelligent, automated wrapper around `bwrap`, translating high-level semantic rules into low-level bubblewrap command-line invocations.

### Consequences
#### Positive
- **Near-zero overhead:** Relies on Linux unprivileged user namespaces (`CLONE_NEWUSER`, `CLONE_NEWNS`, etc.) without daemon or image overhead.
- **Rootless execution:** Operates entirely in unprivileged user space.
- **Granular control:** Allows fine-grained path binds (`--ro-bind`, `--bind`, `--tmpfs`, `--dev`, `--proc`).
- **Host runtime reuse:** Can transparently expose installed host tools (`/usr`, `/bin`) without container image builds.

#### Negative
- **Linux only:** Confined to Linux kernels supporting unprivileged user namespaces.
- **Host dependency:** Requires `bwrap` to be installed on the host machine.

---

## ADR-0002: Go (Golang) as the Implementation Language

### Status
Accepted

### Context
`bwrun` is a CLI tool invoked before running developer commands and agents. It must satisfy:
- Minimal startup latency (< 10ms).
- Zero external runtime dependencies (no JVM, Python interpreter, or Node runtime).
- Cross-architecture single-binary distribution.
- Robust POSIX/Linux process, signal, and filesystem primitives.

### Decision
Implement `bwrun` in Go (Golang).

### Consequences
#### Positive
- Compiles into a single, self-contained static executable with instant startup.
- Standard library provides comprehensive support for process execution (`os/exec`), signals (`os/signal`), POSIX primitives (`syscall`), and configuration parsing (`encoding/json`).
- Minimal maintenance overhead without dynamic shared-library linking issues.

#### Negative
- Native Linux system call interfaces occasionally require platform-specific build tags (`//go:build linux`).

---

## ADR-0003: Three-Tier Semantic Layering Model

### Status
Accepted

### Context
Manually mapping filesystem paths for bubblewrap is error-prone. A developer often needs to expose:
1. System binaries and libraries.
2. Developer tool configurations, runtimes, and skills.
3. The current project workspace.
At the same time, sensitive personal files (`~/.ssh`, `~/.gnupg`, `~/.aws`, other projects) must remain strictly hidden.

### Decision
Adopt a three-tier semantic layering model:
1. **Base Layer (System Infrastructure):** The host root filesystem (`/`) is visible at its normal paths and read-only. `/dev` and `/proc` are instantiated by bwrap, and `/tmp` is private temporary storage.
2. **Config Layer (Developer Tools & Dotfiles):** Existing top-level dotfiles and dotdirectories are mounted Read-Only by default. XDG config/cache/data/state roots and known package caches receive Read-Write mounts; known credential and history paths are denied.
3. **User Layer (Workspace & Personal Data):** `$HOME` is denied/hidden by default. Only the Current Working Directory (CWD) is mounted as Read-Write (`--bind`), along with explicitly whitelisted paths.

Additionally, support **Shadow Mounts** to allow substituting sensitive target paths (e.g., replacing real `~/.ssh` with isolated dummy or project-specific keys).

### Consequences
#### Positive
- Secure by default: Agent can only modify files inside the project working directory and explicitly writable paths; the rest of the host root remains read-only.
- Global tool skills and configs remain readable without vulnerability to tampering or deletion.
- Prevents exfiltration of host secrets located in personal home subdirectories.

#### Negative
- Tools that insist on writing state to non-cache dotfiles in `$HOME` may fail unless explicitly configured in user/project configs.

---

## ADR-0004: JSON as the Primary Configuration Format

### Status
Accepted

### Context
`bwrun` requires declarative configuration at both the user level (`~/.config/bwrun/config.json`) and the project level (`<project-root>/.bwrun.json`).
While YAML is often used for developer configs, parsing YAML in Go requires third-party dependencies (such as `gopkg.in/yaml.v3`). JSON is supported directly in the Go standard library (`encoding/json`), produces zero external dependency footprint, and is readily validated with JSON Schema.

### Decision
Adopt standard JSON as the official configuration format for both `.bwrun.json` and user-level configs.
Configuration resolution follows a strict hierarchy (highest to lowest):
1. Command Line Flags (`--rw`, `--ro`, `--deny`, etc.)
2. Project Configuration (`<cwd>/.bwrun.json`)
3. User Global Configuration (`~/.config/bwrun/config.json`)
4. Built-in Semantic Rules & Catalog Defaults

### Consequences
#### Positive
- Zero external package dependencies for configuration parsing in Go.
- Easy integration with IDEs, JSON Schema validation, and automated tooling.
- Unambiguous parsing semantics.

#### Negative
- Standard JSON does not natively support inline comments. (If needed in future phases, a comment-tolerant parser or alternate format support can be evaluated).

---

## ADR-0005: Runtime Existence-Checking for Dynamic Mount Construction

### Status
Accepted

### Context
`bwrap` fails with an immediate fatal error if an argument specifies a nonexistent source path (e.g. `--ro-bind ~/.cargo ~/.cargo` when Rust is not installed).
The built-in catalog will contain a predefined, hardcoded list of common tools and dotfiles (Node, Rust, Go, Python, AI agents like Goose/Gemini). However, individual developer environments differ significantly and only a subset of these paths exist on any given machine.

### Decision
`bwrun` will evaluate all candidate mount paths at runtime:
1. Maintain an internal curated catalog of well-known runtime and agent paths.
2. Expand paths (resolving `~` and environment variables).
3. Check path existence on the host filesystem (`os.Stat` / `os.Lstat`).
4. Dynamically append only verified, existing paths as `--ro-bind` or `--bind` arguments to the `bwrap` command. Paths that do not exist on the host are skipped silently without causing execution errors.
5. In future phases, consider heuristics or LLM-assisted evaluation for unknown dotfiles/directories in `$HOME`.

### Consequences
#### Positive
- Prevents `bwrap` failure caused by missing catalog entries.
- Out-of-the-box functionality across diverse machine setups without requiring user intervention.
- Clean separation between catalog knowledge (declarative listing) and execution planning (dynamic path resolution).

#### Negative
- Minor filesystem stat overhead at startup (negligible for dozens of paths in local filesystem cache).

---

## ADR-0006: Child Process Execution with Transparent I/O and Signal Forwarding

### Status
Accepted

### Context
`bwrun` is primarily designed for running AI agents, but also interactive shells (`bash`), CLI linters, and compilers. 
The user experience must be indistinguishable from native terminal execution:
- Standard input, output, and error streams must pass through transparently.
- Terminal control (TTY / raw mode / cursor keys / interactive prompts) must work as expected.
- Signals (such as `SIGINT` from Ctrl+C, `SIGTERM`, `SIGHUP`) must reach the sandboxed process.
- Exit code must match the executed command.

While `syscall.Exec` directly replaces the runner process, managing `bwrap` as a child process with signal interception allows lifecycle monitoring, future pre/post hooks, diagnostic logging, and clean cleanup.

### Decision
Execute `bwrap` as a child process (`os/exec.Command`) with:
1. `Stdin`, `Stdout`, and `Stderr` directly attached to `os.Stdin`, `os.Stdout`, and `os.Stderr`.
2. A signal relay channel catching `SIGINT`, `SIGTERM`, `SIGHUP`, and `SIGQUIT`, forwarding them immediately to the child process.
3. Synchronous wait for process termination and propagation of the exact child exit status code to `os.Exit()`.

### Consequences
#### Positive
- Standard CLI interactions and full-screen / raw terminal UIs function seamlessly.
- Enables lifecycle extensions (Phase 3 pre-exec and post-exec hooks, audit logging).
- Clean exit code parity for CI/CD and script automation.

#### Negative
- `bwrun` process stays resident in memory for the duration of the command (overhead is minimal, typically < 10MB RSS for Go).

---

## ADR-0007: Default Host Network Passthrough with Opt-Out Flag

### Status
Accepted

### Context
Modern AI agents (Goose, Gemini CLI, Claude Code) rely on network access for:
- Remote LLM inference APIs (Google Cloud, OpenAI, Anthropic, etc.).
- Package dependencies download (`npm`, `go get`, `cargo`, `pip`).
- Web browsing and search tools.
Completely isolating the network by default would break virtually every agent run without heavy setup.

### Decision
Enable host network access by default (sharing the host network namespace). Provide an explicit flag (`--no-net` or `"network": "deny"` in `.bwrun.json`) for hermetic or offline sandboxed tasks.

### Consequences
#### Positive
- Out-of-the-box support for AI agents and network-dependent developer commands.
- Zero extra proxy or virtual network configuration required for standard usage.

#### Negative
- An agent could theoretically send data over the network if it gained access to sensitive data (mitigated by strict User Layer filesystem denial).
- Network filtering/proxying is deferred to Phase 3.

---

## ADR-0008: Default Environment Variable Pass-Through with Explicit Deny/Override

### Status
Accepted

### Context
Modern AI agents, developer tools, and language runtimes rely extensively on host environment variables for operation (e.g., LLM API keys such as `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, runtime path configurations like `NVM_DIR`, `GOPATH`, proxy settings, and locale).
Filtering environment variables via a strict whitelist by default introduces high configuration friction, breaking the "zero-configuration" developer experience of running `bwrun <command>` seamlessly out of the box.

### Decision
Inherit all host environment variables by default.
Provide explicit configuration to deny or override specific environment variables:
1. **Default Pass-through:** Host environment variables that are set are passed to the sandboxed process.
2. **Explicit Deny (`env.deny`):** Allow users to intentionally filter out sensitive environment variables (e.g., cloud credentials, deployment tokens) via `.bwrun.json`.
3. **Explicit Pass (`env.pass`):** Retain the option to name variables for clarity; set host values are already inherited by default.
4. **Explicit Override (`env.set`):** Allow setting or overriding specific environment variables.
5. **Mandatory Runtime Variables:** `bwrun` always guarantees essential sandbox environment variables (such as setting `HOME` to the sandbox home path and `BWRUN_SANDBOX=1`).

### Consequences
#### Positive
- Zero friction for AI agents and developer tools requiring pre-configured API keys and environment settings.
- Aligns with the default host network passthrough philosophy (ADR-0007) of prioritizing developer productivity out of the box.
- Developers maintain control to hide sensitive secrets when needed via `env.deny`.

#### Negative
- Host secrets stored in environment variables (e.g., accidental export of tokens in a shell session) are visible to the sandboxed process unless explicitly listed in `env.deny`.
