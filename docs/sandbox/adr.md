# Architecture Decision Records (ADR): Sandbox & Isolation (`/sandbox`)

This document records the architectural decisions for filesystem layering, dynamic mount construction, and namespace/persistence hardening in `bwrun` (bubblewrap-runner).

- **Product-Wide ADRs:** [../adr.md](../adr.md)
- **Configuration & Environment ADRs:** [../config/adr.md](../config/adr.md)
- **Sandbox & Layering PRD:** [prd.md](prd.md)

---

## Table of Contents

- [ADR-0003: Three-Tier Semantic Layering Model](#adr-0003-three-tier-semantic-layering-model)
- [ADR-0005: Runtime Existence-Checking for Dynamic Mount Construction](#adr-0005-runtime-existence-checking-for-dynamic-mount-construction)
- [ADR-0009: PID/IPC Namespace Isolation and Persistence Hardening](#adr-0009-pidipc-namespace-isolation-and-persistence-hardening)

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
Adopt a three-tier semantic layering model (Base Layer, Config Layer, User Layer) and Project Sandbox Overlays.
* **Base Layer:** Host root (`/`) is visible read-only; `/dev` and `/proc` are instantiated by bwrap; `/tmp` is private.
* **Config Layer:** Top-level dotfiles/dotdirectories are mounted read-only by default; standard XDG paths and package caches are writable; known credentials and persistence vectors are denied.
* **User Layer:** The host `$HOME` is masked at its original path; the Current Working Directory (CWD) is mounted read-write (`--bind`), with runner configurations protected as read-only.
  * **Workspace Boundary Validation:** Starting in `/`, `$HOME`, or any ancestor directory containing `$HOME` is unconditionally rejected. To prevent accidentally exposing broad parent directories (such as `~/src`) as a writable workspace, `bwrun` verifies that the starting CWD is inside a project boundary by searching upward from CWD (stopping before `$HOME` and `/`) for a configured project marker (default `[".git"]`, configurable via `"workspace-markers"`, plus `.bwrun.json`). When a marker is found in CWD or an ancestor below `$HOME`, only the starting CWD is mounted read-write. Running outside a marked project requires passing `--allow-cwd` explicitly.
* **Project Sandbox Overlays:** Project-specific isolated credentials under `.bwrun/sandbox/` automatically overlay sensitive home paths (e.g. `~/.ssh`).
* **Explicit Home Overlay Selection:** `--sandbox-dir <dir>` and the top-level `"sandbox-dir"` configuration key select the overlay directory explicitly, replacing automatic discovery. CLI selection overrides configuration selection. Relative paths for either selection are resolved from the starting CWD, so a shared tool profile can reference the current workspace’s overlay. This allows a tool profile chosen with `--config` to use isolated home files without implicitly loading the surrounding project's settings or credentials. Without explicit directory selection, `--config` continues to disable automatic overlay discovery.
* **Explicit Nested Writable Bindings:** `mounts.rw` accepts source/target objects as well as existing string paths. Parent host mounts remain shared while selected children use persistent project-local storage. For example, `~/.copilot` can retain host configuration while `session-state` is bound from `.bwrun/sandbox/.copilot/session-state`. Relative object paths use the starting CWD so profiles outside a project can isolate its state. Normal destination precedence and parent-before-child ordering are retained; runner policy files cannot be exposed through writable source aliases.

For complete, authoritative specifications of the layering rules, catalog entries, and precedence, see [docs/sandbox/prd.md](prd.md) and [docs/config/prd.md](../config/prd.md).

### Consequences
#### Positive
* Writable access is confined strictly to the project working directory, auto-writable XDG directories/caches, project sandbox entries, and explicit rules.
* Automatically exposed dotfiles remain readable while being protected against tampering or deletion.
* Known credential and persistence paths remain hidden; unrelated home repositories and personal files remain invisible.
* Prevents cross-project source code leakage and sensitive credential compromise.

#### Negative
* Tools requiring write access to legacy non-cache paths under `$HOME` require explicit configuration.

---

## ADR-0005: Runtime Existence-Checking for Dynamic Mount Construction

### Status
Accepted

### Context
`bwrap` fails with an immediate fatal error if an argument specifies a nonexistent source path (e.g. `--ro-bind ~/.cargo ~/.cargo` when Rust is not installed).
The built-in catalog will contain a predefined list of common tools and dotfiles (Node, Rust, Go, Python, AI agents like Goose/Gemini). However:
1. Individual developer environments differ significantly, and only a subset of these paths exist on any given machine.
2. Hardcoding catalog paths tightly into Go control flow creates maintenance debt as the ecosystem of developer tools and AI agents evolves.

### Decision
`bwrun` will evaluate all candidate mount paths at runtime while decoupling catalog knowledge from path resolution:
1. **Declarative Catalog Representation:** Structure catalog entries as declarative data (e.g., embedded JSON or declarative structs), keeping semantic rules cleanly separated from bwrap invocation logic.
2. **Runtime Existence Evaluation:** Expand paths (resolving `~` and environment variables) and check existence on the host filesystem (`os.Stat` / `os.Lstat`). Dynamically append only verified, existing paths as `--ro-bind` or `--bind` arguments to the `bwrap` command. Nonexistent paths are skipped silently.
3. **Future Extensibility:** Design catalog interfaces to allow loading external catalog definitions (e.g., from `~/.config/bwrun/catalog.d/` or remote data updates) and user-defined catalog overrides without requiring binary recompilation.
4. **Diagnostic Guidance (`bwrun doctor`):** Rather than performing non-deterministic runtime heuristics or LLM evaluation for unrecognized dotfiles/directories in `$HOME`, provide a deterministic diagnostic command (`bwrun doctor`). It reports uncataloged dotpaths and guides users to configure them via `catalog.d/` or configuration files if specific permissions (e.g., write access for caches or explicit denials for secrets) are required.

### Consequences
#### Positive
- Prevents `bwrap` failure caused by missing catalog entries.
- Out-of-the-box functionality across diverse machine setups without requiring user intervention.
- Clean separation between catalog knowledge (declarative listing) and execution planning (dynamic path resolution).
- Facilitates external catalog data maintenance and user-defined catalog extensions as new tools emerge.

#### Negative
- Minor filesystem stat overhead at startup (negligible for dozens of paths in local filesystem cache).

---

## ADR-0009: PID/IPC Namespace Isolation and Persistence Hardening

### Status
Accepted

### Context
When running untrusted developer tools or autonomous AI agents, sandboxed processes can potentially attempt to:
1. **Inspect and tamper with host processes:** Without namespace isolation, processes in the sandbox can view host processes via `/proc`, attempt inter-process signaling (sending signals like `SIGKILL` or `SIGSTOP`), or interact with host IPC primitives (SysV shared memory, POSIX message queues, semaphores).
2. **Subvert runner configuration (Privilege Escalation):** Because the current working directory (CWD) is mounted Read-Write (`--bind`), a malicious or errant agent could overwrite `<cwd>/.bwrun.json` or files under `<cwd>/.bwrun/` to weaken security boundaries (e.g., granting write permissions to sensitive host paths or removing denials) for subsequent runs.
3. **Establish host persistence (Autostart Backdoors):** Because standard XDG directories under `~/.config` are writable for normal application state, an agent could plant autostart triggers (such as `~/.config/autostart/*.desktop` or `~/.config/systemd/user/*.service`) that execute arbitrary code outside the sandbox upon user login or user session startup.

### Decision
Implement three defense-in-depth isolation controls:
1. **Unshared PID & IPC Namespaces by Default:**
   - Always invoke `bwrap` with `--unshare-pid` and `--unshare-ipc`.
   - Mount an isolated, private `/proc` inside the sandbox so only processes within the sandbox are visible and controllable.
2. **Self-Protection of Runner Configurations in CWD:**
   - After mounting CWD as Read-Write (`--bind`), automatically re-mount any existing `.bwrun.json`, `.bwrun.local.json`, and `.bwrun/` directory as Read-Only (`--ro-bind`).
   - Reject symbolic links at these policy paths, including project or global configuration files, because protecting a link target does not protect the link itself from replacement.
3. **Automatic Denial of Autostart / Persistence Paths:**
   - Include `~/.config/autostart` and `~/.config/systemd/user` in the built-in deny list. Even though parent `~/.config` may be writable, these critical persistence vector paths are masked/denied inside the sandbox.

### Consequences
#### Positive
- Prevents cross-process snooping, signal injection, and IPC interference between sandbox processes and host processes.
- Eliminates the risk of agents modifying their own sandbox policy to escalate privileges on subsequent invocations.
- Closes the primary vector for persistent malware/backdoor installation on modern Linux desktop and systemd user environments.
- Zero configuration required from the user to benefit from these protections.

#### Negative
- Sandboxed processes cannot intentionally configure user autostart services or systemd user units without explicit configuration override.
- Configuration files (`.bwrun.json`) cannot be modified directly from inside the sandbox; modifications must occur on the host.
