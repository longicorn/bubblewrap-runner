# Troubleshooting & Common Issues

This guide addresses common runtime issues and environmental constraints encountered when running `bwrun` and `bubblewrap`.

---

## 1. Unprivileged User Namespaces Restrictions (AppArmor / Ubuntu 24.04+)

### Symptom
When executing `bwrun`, commands fail immediately with errors from bubblewrap such as:
```text
bwrap: setting up uid map: Permission denied
```
or
```text
bwrap: creating new namespace failed: Operation not permitted
```

### Cause
Modern Linux distributions (such as Ubuntu 24.04 LTS and newer, Debian 12+) enforce kernel and AppArmor restrictions on unprivileged user namespaces (`userns`) to mitigate potential local privilege escalation attacks. Because `bubblewrap` relies on unprivileged user namespaces to set up sandboxes without root privileges, execution is blocked unless explicitly permitted.

> **Note:** This is an operating system and kernel security restriction affecting `bubblewrap` itself, not a bug in `bwrun`.

### Solutions

Choose one of the following approaches based on your security requirements and installation path:

#### Option A: Allow User Namespaces for `bwrap` via AppArmor Profile (Recommended)
This approach restricts unprivileged user namespace creation specifically to your `bwrap` binary without weakening protections across the rest of the system.

1. **Locate your `bwrap` binary:**
   Determine the absolute path of the `bwrap` executable being used:
   ```bash
   which bwrap
   ```
   *(e.g., `/usr/bin/bwrap`, `/usr/local/bin/bwrap`, or `~/.local/bin/bwrap`)*

2. **Create an AppArmor profile:**
   Create a profile file under `/etc/apparmor.d/` (for example, `/etc/apparmor.d/bwrap`). Replace `<PATH_TO_BWRAP>` with the absolute path found in step 1:
   ```text
   abi <abi/4.0>,
   include <tunables/global>

   profile bwrap <PATH_TO_BWRAP> flags=(unconfined) {
     userns,
   }
   ```
   *Note: If your binary is located in your home directory (e.g., `/home/<user>/.local/bin/bwrap`), ensure the full path without `~` is specified.*

3. **Load the profile:**
   ```bash
   sudo apparmor_parser -r /etc/apparmor.d/bwrap
   ```

#### Option B: Disable System-wide Restriction via sysctl (Alternative / Development Setup)
If you frequently test custom binaries or want a simpler setup in private development environments, you can disable the kernel restriction globally:

1. **Apply temporarily:**
   ```bash
   sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0
   ```

2. **Persist across reboots:**
   Create `/etc/sysctl.d/60-apparmor-namespace.conf` with:
   ```ini
   kernel.apparmor_restrict_unprivileged_userns = 0
   ```
   *Warning: This disables unprivileged user namespace restrictions globally across all applications on the host.*

---

## 2. Missing `bwrap` Executable

### Symptom
```text
exec: "bwrap": executable file not found in $PATH
```

### Solution
Ensure `bubblewrap` is installed and its binary directory is included in your `$PATH`.

`bwrap` can typically be installed via your system package manager (e.g., `apt`, `pacman`, `dnf`), Homebrew on Linux (`brew install bubblewrap`), or built from source.

For detailed and distribution-specific installation instructions, refer to the [upstream Bubblewrap documentation](https://github.com/containers/bubblewrap).
