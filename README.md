# bubblewrap-runner

`bubblewrap-runner` starts a command in a Bubblewrap sandbox. The host filesystem remains visible at its normal paths, read-only by default. The current working directory is writable. Existing top-level dotfiles and dotdirectories are visible read-only; XDG config/cache/data/state directories and known package caches are writable. Known credential and shell-history paths stay hidden.

## Prerequisites

`bubblewrap-runner` is a wrapper around [Bubblewrap](https://github.com/containers/bubblewrap) (`bwrap`). You must have `bwrap` installed on your host system.

Run commands with `bwrun`, not `bwrap` directly. For example, use `bwrun bash` to start a shell. A bare `bwrap bash` does not mount the host filesystem, so Bubblewrap cannot find `bash` and reports `execvp bash: No such file or directory`.

## Usage

```sh
bwrun init
bwrun bash
bwrun --ro ~/docs/shared -- bash
bwrun --dry-run -- bash
bwrun --config ~/.config/bwrun/profiles/agent.json -- bash
```

Commands inside the sandbox receive `BWRUN_SANDBOX=1`. Add this at the end of `~/.bashrc` to mark an interactive Bash prompt:

```bash
if [[ ${BWRUN_SANDBOX:-} == 1 ]]; then
  PS1="[bwrun] ${PS1}"
fi
```

`bwrun init` creates a project `.bwrun.json` template and leaves an existing file untouched. Project settings go in `.bwrun.json`; user settings go in `~/.config/bwrun/config.json`:

Project-local home files can be supplied without adding mount entries: put them under `.bwrun/sandbox/` using their home-relative paths. For example, `.bwrun/sandbox/.ssh` is mounted read-write at `~/.ssh`; any existing entries directly under `.bwrun/sandbox/` are mounted the same way automatically. Explicit project or command-line mount rules can override these defaults.

```json
{
  "version": "1",
  "network": "allow",
  "mounts": {
    "ro": ["~/.config/my-tool"],
    "rw": ["~/.cache/my-tool"],
    "deny": ["~/.aws"]
  },
  "env": {
    "pass": ["XXXX_API_KEY"],
    "deny": [],
    "set": {"CI": "true", "EDITOR": "vim"}
  }
}
```

Install Bubblewrap (`bwrap`) on Linux, then build with `go build ./cmd/bwrun`.
Host environment variables are inherited by default. Use `env.deny` to filter sensitive variables, `env.pass` to explicitly pass a variable, or `env.set` to set or override a value. To use a tool installed under the home directory, add its binary directory to `mounts.ro` (or `mounts.rw` if it needs to write there).

## Tool-specific environment profiles

Managing sensitive tokens and environment variables often falls between two extremes:
* **Shell-wide exports (`~/.bashrc` / `~/.zshrc`):** Exposes credentials and API tokens to every process launched in that shell session.
* **Directory-based environment managers (e.g., `direnv`, `.env` loaders):** Tie environment variables to specific working directories, making them awkward for general-purpose developer tools and AI agents invoked from arbitrary locations. Furthermore, they only manage environment variables without restricting filesystem access.

`bwrun` functions as a **tool-scoped environment manager** alongside filesystem sandboxing. It lets you isolate and inject environment variables exclusively for a target command, without leaking secrets across directories or exposing them to other processes. For tools you invoke across multiple projects, maintain a private environment profile outside project trees and select it at launch:

```sh
# ~/.config/bwrun/profiles/agent.json
{
  "version": "1",
  "env": {
    "pass": [],
    "deny": ["UNRELATED_TOKEN"],
    "set": {"EXAMPLE_API_TOKEN": "<token for this tool>"}
  }
}
```

```sh
alias myagent='bwrun --config ~/.config/bwrun/profiles/agent.json -- myagent'
```

`--config` loads only the named configuration file; it does not load the usual global or nearest-project configuration, or the nearest project's `.bwrun/sandbox/` entries. Built-in mount rules and command-line flags still apply. A relative `--config` path is resolved from the starting directory; paths inside that file are resolved from the file's directory. The file must exist and cannot be a symbolic link. The selected file is hidden inside the sandbox so the launched command cannot change the policy for a later run.

The host environment is still inherited unless names are listed in `env.deny`. Explicit `env.set` values apply after this filtering: a name listed in both uses the configured value, including an empty string. Across configuration layers, `env.deny` filters host inheritance only; `env.set` values follow configuration-layer precedence. `env.set` values are stored as plain text, so keep files containing secrets private (for example, mode `0600`) and out of version control. `env.deny` filters the initial environment only: an interactive shell can export the same name again from its startup files. Remove sensitive exports from `.bashrc` when using this pattern.

## Documentation

For authoritative and detailed specifications:
- [Product Requirements Document (PRD)](docs/prd.md): Comprehensive specifications on filesystem layering, configuration schema, environment precedence, and the built-in tool catalog.
- [Architecture Decision Records (ADR)](docs/adr.md): Architectural decisions and rationale covering Bubblewrap adoption, security hardening, process execution, and configuration resolution.

