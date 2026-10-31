# Architecture Decision Records (ADR): Configuration & Environment (`/config`)

This document records the architectural decisions for configuration format, hierarchy, and environment variable handling in `bwrun` (bubblewrap-runner).

- **Product-Wide ADRs:** [../adr.md](../adr.md)
- **Sandbox & Isolation ADRs:** [../sandbox/adr.md](../sandbox/adr.md)
- **Configuration & Environment PRD:** [prd.md](prd.md)

---

## Table of Contents

- [ADR-0004: JSON as the Primary Configuration Format](#adr-0004-json-as-the-primary-configuration-format)
- [ADR-0008: Default Environment Variable Pass-Through with Explicit Deny/Override](#adr-0008-default-environment-variable-pass-through-with-explicit-denyoverride)

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

For complete details on configuration schema, flag precedence, and path resolution rules, see [docs/config/prd.md](prd.md).

### Consequences
#### Positive
- Zero external package dependencies for configuration parsing in Go.
- Easy integration with IDEs, JSON Schema validation, and automated tooling.
- Unambiguous parsing semantics.

#### Negative
- Standard JSON does not natively support inline comments. (If needed in future phases, a comment-tolerant parser or alternate format support can be evaluated).

---

## ADR-0008: Default Environment Variable Pass-Through with Explicit Deny/Override

### Status
Accepted

### Context
Modern AI agents, developer tools, and language runtimes rely extensively on host environment variables for operation (e.g., LLM API keys such as `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, runtime path configurations like `NVM_DIR`, `GOPATH`, proxy settings, and locale).
Filtering environment variables via a strict whitelist by default introduces high configuration friction, breaking the "zero-configuration" developer experience of running `bwrun <command>` seamlessly out of the box.

### Decision
Inherit host environment variables by default while providing granular deny and override controls:
1. **Default Pass-through:** Set host environment variables are inherited automatically.
2. **Explicit Deny (`env.deny`):** Filter out sensitive environment variables (cloud credentials, deployment tokens, etc.).
3. **Explicit Pass (`env.pass`):** Explicitly retain specific environment variables.
4. **Explicit Override (`env.set`):** Set or override environment variables.
5. **Mandatory Variables:** `bwrun` guarantees essential sandbox variables (e.g., `HOME` and `BWRUN_SANDBOX=1`).

For detailed precedence and resolution behavior, see [docs/config/prd.md](prd.md) Section 1.4. Note: To further support tool-scoped profiles and prevent secret exfiltration, a strict non-inheriting clean environment mode (`--clean-env`) and external environment files (`env_file`) are planned for Phase 2.

### Consequences
#### Positive
- Zero friction for AI agents and developer tools requiring pre-configured API keys and environment settings.
- Aligns with the default host network passthrough philosophy ([ADR-0007](../adr.md#adr-0007-default-host-network-passthrough-with-opt-out-flag)) of prioritizing developer productivity out of the box.
- Developers maintain control to hide sensitive secrets when needed via `env.deny`.

#### Negative
- Host secrets stored in environment variables (e.g., accidental export of tokens in a shell session) are visible to the sandboxed process unless explicitly listed in `env.deny`.

#### Future Considerations
- **External Environment Files (`env_file`):** Support referencing dedicated external environment files (e.g. `.env`) rather than inlining values directly in JSON configurations.
- **External Secret Management Integration:** Rather than embedding complex encryption/decryption logic into `bwrun` itself, keep the core focused on isolation and recommend standard secret managers or encryption tools (such as `sops`, `vault`, or `direnv`) to populate the environment prior to `bwrun` execution.
