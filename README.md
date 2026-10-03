# bwrun

`bwrun` starts a command in a Bubblewrap sandbox. The host filesystem remains visible at its normal paths, read-only by default. The current working directory is writable, while other paths under the user's home directory stay hidden unless explicitly allowed.

```sh
bwrun init
bwrun bash
bwrun --ro ~/docs/shared -- gemini
bwrun --dry-run -- bash
```

`bwrun init` creates a project `.bwrun.json` template and leaves an existing file untouched. Project settings go in `.bwrun.json`; user settings go in `~/.config/bwrun/config.json`:

```json
{
  "version": "1",
  "network": "allow",
  "mounts": {
    "ro": ["~/.config/goose/recipes"],
    "rw": ["~/.cache/my-tool"],
    "deny": ["~/.aws"]
  },
  "env": {
    "pass": ["GOOGLE_API_KEY"],
    "set": {"CI": "true"}
  }
}
```

Install Bubblewrap (`bwrap`) on Linux, then build with `go build ./cmd/bwrun`.
Host PATH entries outside the home directory remain available. To use a tool installed under the home directory, add its binary directory to `mounts.ro` (or `mounts.rw` if it needs to write there); variables such as API keys must be listed in `env.pass`.
