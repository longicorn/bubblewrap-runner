# Product Requirements Document (PRD): Configuration & Environment (`/config`)

This document defines the configuration hierarchy, path resolution rules, schema reference, and environment variable controls for `bwrun` (bubblewrap-runner).

- **Product Overview, CLI & Roadmap:** [../prd.md](../prd.md)
- **Sandbox & Layering Specifications:** [../sandbox/prd.md](../sandbox/prd.md)
- **Configuration & Environment ADRs:** [adr.md](adr.md)

---

## 1. Configuration Model & Precedence

### 1.1 Configuration Hierarchy
Configuration is resolved in the following priority order (highest to lowest):
1. **Command Line Flags:** Explicit runtime arguments (e.g., `--rw <path>`, `--ro <path>`, `--deny <path>`).
2. **Project-level Configuration:** `<project-root>/.bwrun.json`.
3. **User Global Configuration:** `~/.config/bwrun/config.json`.
4. **Built-in Semantic Rules & Catalog:** Hardcoded defaults and built-in tool path catalog (see [../sandbox/prd.md](../sandbox/prd.md)).

### 1.2 Explicit Configuration File Mode (`--config <file>`)
When `--config <file>` is given, only that file is used as the configuration layer. The global and nearest-project configuration files are not discovered, and nearest-project `.bwrun/sandbox/` entries are not automatically injected. Home overlays remain available through an explicit `--sandbox-dir` or `"sandbox-dir"` selection, or specific source/target bindings in `mounts.rw`. Built-in semantic rules and CLI flags still apply. The file must exist and must not be a symbolic link. Relative `--config` paths are resolved from the starting directory, while string mount paths inside the file are resolved from the file's directory. Relative `sandbox-dir` paths and source/target object paths are resolved from the starting CWD. The selected file is hidden inside the sandbox to prevent changes to the policy for later runs.

### 1.3 Mount Precedence, Path Resolution, & Workspace Markers
* **Mount Precedence & Ordering:** For mount rules, settings from a higher layer replace lower-layer rules for the same destination; nested paths are applied from parent to child.
* **Relative Path Resolution:**
  * String paths in project configuration are relative to the directory containing `.bwrun.json`.
  * String paths in global configuration are relative to the user's home.
  * String paths inside a `--config <file>` configuration are relative to that file's directory.
  * CLI string paths are relative to the starting directory (CWD).
  * Relative `sandbox-dir` paths and `mounts.rw` `{source, target}` object paths are always relative to the starting directory (CWD) across all layers.
* **Workspace Boundary Markers (`"workspace-markers"`):** The optional top-level `"workspace-markers"` key accepts a non-empty array of single entry names (base names without path separators or `..`, defaulting to `[".git"]`) used during upward project boundary discovery; a higher configuration layer's `"workspace-markers"` list replaces lower layers, while `.bwrun.json` remains recognized as a project boundary.

### 1.4 Environment Variable Resolution
* **Mandatory Sandbox Variables:** `HOME` is always set to the sandbox home path, and `BWRUN_SANDBOX=1` is always set so shells and scripts can identify sandbox execution.
* **Inheritance, Deny, Set, & Pass:** Existing host environment variables are passed through by default; `env.deny` filters inherited host variables, then `env.set` sets or overrides values. If a name appears in both, the explicit `env.set` value is used, including an empty string. Denials from any configuration layer apply only to host inheritance; explicit set values follow configuration-layer precedence. `env.pass` can explicitly request a variable that is otherwise absent from the host environment when available.

---

## 2. Configuration Schema & Sample Reference (`.bwrun.json.sample`)

A comprehensive sample file, `.bwrun.json.sample`, is maintained in the repository root to illustrate all supported top-level and nested configuration fields (while `bwrun init` generates a minimal starter `.bwrun.json` with empty rules):

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "version": "1",
  "network": "allow",
  "workspace-markers": [
    ".git"
  ],
  "sandbox-dir": ".bwrun/sandbox",
  "mounts": {
    "ro": [
      "~/.config/goose/recipes",
      "~/.local/share/agent-skills"
    ],
    "rw": [
      "~/.cache/my-tool",
      {
        "source": ".bwrun/sandbox/.copilot/session-state",
        "target": "~/.copilot/session-state"
      }
    ],
    "deny": [
      "~/.aws",
      "~/.config/gcloud"
    ]
  },
  "env": {
    "pass": [],
    "deny": [
      "AWS_SECRET_ACCESS_KEY",
      "GITHUB_TOKEN"
    ],
    "set": {
      "CI": "true"
    }
  }
}
```
