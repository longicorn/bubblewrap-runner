# Architecture Decision Records (ADR) - bwrun (bubblewrap-runner)

This document records the product-wide architectural and design decisions made for `bwrun` (bubblewrap-runner), and indexes domain-specific ADRs under `docs/sandbox/` and `docs/config/`.

---

## Table of Contents

### Product & Execution (`docs/adr.md`)
- [ADR-0001: Bubblewrap (bwrap) as the Core Isolation Engine](#adr-0001-bubblewrap-bwrap-as-the-core-isolation-engine)
- [ADR-0002: Go (Golang) as the Implementation Language](#adr-0002-go-golang-as-the-implementation-language)
- [ADR-0006: Child Process Execution with Transparent I/O and Signal Forwarding](#adr-0006-child-process-execution-with-transparent-io-and-signal-forwarding)
- [ADR-0007: Default Host Network Passthrough with Opt-Out Flag](#adr-0007-default-host-network-passthrough-with-opt-out-flag)

### Sandbox & Isolation ([`docs/sandbox/adr.md`](sandbox/adr.md))
- [ADR-0003: Three-Tier Semantic Layering Model](sandbox/adr.md#adr-0003-three-tier-semantic-layering-model)
- [ADR-0005: Runtime Existence-Checking for Dynamic Mount Construction](sandbox/adr.md#adr-0005-runtime-existence-checking-for-dynamic-mount-construction)
- [ADR-0009: PID/IPC Namespace Isolation and Persistence Hardening](sandbox/adr.md#adr-0009-pidipc-namespace-isolation-and-persistence-hardening)

### Configuration & Environment ([`docs/config/adr.md`](config/adr.md))
- [ADR-0004: JSON as the Primary Configuration Format](config/adr.md#adr-0004-json-as-the-primary-configuration-format)
- [ADR-0008: Default Environment Variable Pass-Through with Explicit Deny/Override](config/adr.md#adr-0008-default-environment-variable-pass-through-with-explicit-denyoverride)

---

## ADR-0001: Bubblewrap (bwrap) as the Core Isolation Engine

### Status
Accepted

### Context
Developers executing AI coding agents (Goose, Gemini CLI, Claude Code, etc.) or untrusted developer tools on host machines face significant risks of accidental host damage or credential leakage (e.g., `~/.ssh`, `~/.aws`). 

In practice, determined adversarial agents or zero-day payloads have demonstrated escapes even in virtualized and Docker environments; pursuing absolute containment against sophisticated kernel exploits is impractical for local CLI tools. The primary real-world security challenge developers face daily is **cross-project code leakage and credential theft**:
- Developers frequently juggle multiple proprietary or sensitive repositories under `$HOME/src/...`.
- Autonomous AI agents running on the host can unintentionally (or via prompt injection) index, read, or exfiltrate code from unrelated projects or access personal credentials (`~/.ssh`, `~/.aws`, browser cookies).
- Not all AI providers guarantee zero data retention/training, leading organizations to restrict AI usage.

Existing containerization tools like Docker or Podman introduce substantial operational friction for individual developers:
- Heavy startup latency.
- Complicated host tool/runtime sharing (Node, Python, Go, Rust, system packages).
- Tedious mounting of dotfiles, agent skills, and prompt templates.

Direct execution on the host is frictionless but unsafe. We need an unprivileged, low-overhead isolation mechanism on Linux that can selectively expose host resources and eliminate the primary risk of accidental cross-project leakage without container management overhead.

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
