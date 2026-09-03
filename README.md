# SuperCLI

> Current version: **v0.3.1-alpha**

[![CI](https://github.com/2pedrohenrique/SuperCLI/actions/workflows/ci.yml/badge.svg)](https://github.com/2pedrohenrique/SuperCLI/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go)](go.mod)
[![License](https://img.shields.io/badge/license-MIT%20%2B%20Commons%20Clause-EF4444)](LICENSE)

SuperCLI is a configurable developer workstation inside the terminal. It runs
multiple development processes concurrently, keeps each output in an isolated
log view, reports failures through desktop notifications, and turns recurring
workflows into actions—without compiling project-specific knowledge into the
application.

Personal workstation configuration is intentionally ignored by Git because it
can contain private project names, paths, hosts and commands. The repository
ships only the neutral template in `internal/bootstrap/example.yaml`; generate
your own `supercli.yaml` with `supercli --init`.

## What SuperCLI does

- Runs backend, frontend, tests, or other long-running commands concurrently.
- Shows one retained and scrollable log stream per process.
- Classifies output by message severity rather than treating everything written
  to `stderr` as an error. Warnings are yellow, actual failures are red,
  successful startup messages are green, and ordinary output remains neutral.
- Starts one process or an entire workspace and stops the full child process
  tree on Windows and Unix.
- Detects configurable ready/error patterns using regular expressions.
- Sends a desktop notification on the first detected error and on abnormal
  process exit.
- Saves the latest number of lines chosen at runtime into a timestamped `.txt`
  under the configured temporary directory.
- Provides a configurable action palette for editors, Git, cleanup scripts, and
  any executable. Actions can ask for values and substitute `{{input}}` safely
  as individual process arguments.
- Suspends and restores the TUI for interactive actions such as SSH, a project
  shell, Vim, or another terminal program while background services keep
  running and buffering logs. Since v0.2.5 these actions run inside an embedded
  PTY tile instead of replacing the SuperCLI screen.
- Protects destructive or important actions with confirmation.
- Loads every project and workstation detail from YAML.
- Adds project workspaces and processes from inside the TUI with `n`; changes
  are persisted to the active YAML configuration and become runnable instantly.
- Removes selected processes or workspaces through the same project manager.
  Removal affects configuration only and never deletes project files.
- Exposes native Git workflows with `g`: `pull`, `push`, checkout with
  carry-or-stash behavior, feature/hotfix `cb`, and tagged feature/hotfix `cm`.
  `pull all` updates every unique Git repository in the selected workspace on
  the branch currently checked out in that repository.
  `t` is the TUI equivalent of `td`, opening an interactive shell in the
  selected process directory.
- Loads portable community action plugins from versioned YAML manifests. A
  plugin may invoke an executable written in any language without being linked
  into the SuperCLI process.
- Clears retained output for one selected process without stopping it.
- Opens new capture files in Notepad on Windows; other platforms keep reporting
  the saved path without launching a viewer.
- Provides a visible embedded-terminal cursor, modified navigation keys and an
  `F7` fullscreen terminal suitable for editors such as Neovim. Mouse clicks,
  motion and wheel events are forwarded when the terminal application enables
  mouse tracking.

## Quick start

Requirements: Go 1.26+ and the tools referenced by your configuration.

After the repository is public, a released version can be installed directly:

```sh
go install github.com/2pedrohenrique/SuperCLI/cmd/supercli@v0.3.1-alpha
supercli --init
supercli
```

Clone the repository, then on PowerShell:

```powershell
go test ./...
go build -o .\bin\supercli.exe .\cmd\supercli
if (-not (Test-Path .\supercli.yaml)) { .\bin\supercli.exe --init --config .\supercli.yaml }
.\bin\supercli.exe --config .\supercli.yaml
```

On Linux or macOS:

```sh
go test ./...
go build -o ./bin/supercli ./cmd/supercli
test -f ./supercli.yaml || ./bin/supercli --init --config ./supercli.yaml
./bin/supercli --config ./supercli.yaml
```

To test, build and install the binary plus a neutral starter configuration:

```powershell
.\scripts\install.ps1
```

The script installs to `%LOCALAPPDATA%\SuperCLI\bin` and adds that directory to
the user `PATH`. It sets `SUPERCLI_CONFIG` to the installed configuration and
never overwrites an existing configuration. If tests or compilation fail, the
previously installed executable and configuration remain untouched.

For a neutral starter file:

```powershell
supercli --init --config "$env:LOCALAPPDATA\SuperCLI\home.yaml"
supercli --config "$env:LOCALAPPDATA\SuperCLI\home.yaml"
```

`SUPERCLI_CONFIG` can define the default configuration path. Otherwise SuperCLI
uses `supercli.yaml` in the current directory, then the operating system's user
configuration directory.

## Keyboard map

| Key | Operation |
| --- | --- |
| `[` / `]`, `1`…`9` | Previous / next workspace or jump directly |
| `Tab` / `Shift+Tab` | Next / previous process |
| `Enter` | Start selected process |
| `a` | Start every process in the selected workspace |
| `x` | Stop selected process |
| `X` | Stop all processes |
| `↑`/`↓`, `j`/`k`, `PgUp`/`PgDn` | Scroll logs |
| `Home`, `End`/`f` | Oldest logs / resume following output |
| `F7`, `Esc` | Expand output / return to tiled layout |
| `c` | Ask for N and capture the latest N lines |
| `l` | Clear retained output for the selected process, with confirmation |
| `p` | Open the workspace action palette |
| `n` | Add or remove a project workspace/process and persist it |
| `g` | Native Git menu: pull, push, checkout, cb, cm and td |
| `t` | Focus or create an embedded terminal in the selected directory (`td`) |
| `m` | Return to the SuperCLI home screen |
| `?` | Show in-app help |
| `q` | Quit, confirming when processes are active |

The capture produced by `c` contains metadata and only the selected process's
latest lines. The default path is `%TEMP%\supercli\captures`; Windows opens the
new file in Notepad after saving it.

## Configuration

```yaml
version: 1
capture_dir: "${TEMP}/supercli/captures"
log_buffer_lines: 10000
notifications:
  enabled: true

workspaces:
  - id: my-app
    name: My app
    processes:
      - id: api
        name: API
        command: go
        args: ["run", "."]
        dir: "${USERPROFILE}/Projects/my-app"
        auto_start: false
        ready_patterns: ["(?i)(ready|listening)"]
        error_patterns: ["(?i)(error|panic|build failed)"]
        env:
          APP_ENV: development
    actions:
      - id: feature
        name: Create feature branch
        command: git
        args: ["checkout", "-b", "feature-{{task}}"]
        dir: "${USERPROFILE}/Projects/my-app"
        confirm: true
        interactive: false
        inputs:
          - id: task
            prompt: Task number
```

Paths support environment variables and `~`. The bundled starter configuration
uses portable home-relative paths and leaves the capture directory empty so the
operating-system default is selected. Configure your own project paths before
starting. Commands are launched directly,
not through an implicit shell. This preserves argument boundaries and avoids
shell injection. A shell can still be selected explicitly as an interactive
action; `t` automatically selects the native shell for the embedded terminal.

Set `interactive: true` when the child must own stdin/stdout (for example SSH or
a shell). SuperCLI hosts it through PTY/ConPTY and a VT emulator inside the
focused terminal tile; supervised backend/frontend processes continue running.
While focused, `F6` or `Ctrl+]` detaches without closing the session and
`Ctrl+\` closes it. Clipboard paste and uppercase input through Shift/Caps Lock
are forwarded to the embedded terminal. `Ctrl+A`, `Ctrl+arrows` and other editor
control keys are encoded for the PTY. `F7` toggles fullscreen, and `t` returns
to a detached session.

In the output view, `F7` expands logs to the full window; `F7` or `Esc` restores
the tiles. Process/workspace switching, scrolling, capture (`c`), clearing (`l`)
and other shortcuts remain available. Closing a dialog returns to the same
output layout, without stopping any process.

## Community plugins

The installer creates `%LOCALAPPDATA%\SuperCLI\plugins`, sets
`SUPERCLI_PLUGIN_DIR` and installs the small manifest-only **Git Insights**
plugin. Each plugin is a directory with a strict, versioned `plugin.yaml`; its
contributed actions appear in the normal `p` action palette.

```powershell
supercli --list-plugins
```

The v1 contract, tokens, layout and security model are documented in
[`docs/PLUGINS.md`](docs/PLUGINS.md). Plugins execute with your user permissions,
so install only code you trust. Core contributions follow
[`CONTRIBUTING.md`](CONTRIBUTING.md).

## Architecture

SuperCLI uses a microkernel with dependency direction toward small interfaces:

```text
cmd/supercli (composition root)
        |
        v
  kernel (internal lifecycle)
        |
        +-- workstation plugin
              |-- process supervisor --> OS process adapter
              |-- capture store -------> filesystem
              |-- action runner --------> OS commands
              `-- notifier ------------> desktop adapter
        |
        +-- community plugin host --> manifest actions / executables
        `-- Bubble Tea UI (runtime interface only)
```

The kernel only registers, starts, rolls back, and stops plugins. The workstation
plugin owns the current capabilities. The TUI depends on a `Runtime` interface,
so it can be tested without starting real commands. New capabilities can be
implemented as another `kernel.Plugin` without growing the core.

Important packages:

- `internal/process`: concurrent lifecycle, log multiplexing, state events and
  platform-specific process-tree termination.
- `internal/logbuffer`: bounded, concurrency-safe log retention.
- `internal/config`: validation, defaults, path expansion and action templates.
- `internal/git`: argument-safe native pull, push, branch and commit workflows.
- `pkg/pluginapi`: public, versioned manifest contract for community authors.
- `internal/pluginhost`: discovery, validation, namespacing and adaptation into
  the action palette. Community code remains outside the host process.
- `internal/terminal`: platform-specific interactive project shell (`td`).
  It uses a cross-platform PTY abstraction (ConPTY on Windows) and a VT emulator
  behind a local interface.
- `internal/ui`: Bubble Tea update/view state machine.
- `internal/workstation`: feature facade and microkernel plugin.
- `internal/capture`, `internal/action`, `internal/notify`: replaceable adapters.

## Quality gates

```powershell
go test ./...
go vet ./...
go build ./cmd/supercli
```

Tests cover bounded concurrent logs, severity classification, viewport/modal
dimensions, configuration persistence and templates, capture files, plugin
lifecycle rollback, native Git workflows, and real subprocess
output/error/stop/exit events. The process integration test uses the Go test
binary itself, so it does not rely on Node, .NET, or a project checkout.

## v0.2.0 changes

- Complete responsive UI redesign with workspace sidebar, process tabs, fixed
  log viewport, status line and shortcut bar.
- Help, capture, actions, Git, confirmations and forms render as centered screens
  inside the current terminal dimensions; they are never appended below logs.
- `x` now produces an explicit `stopped` lifecycle state after the process tree
  exits instead of showing a generic finished/failure state.
- Semantic log severity fixes .NET and Angular tools that emit warnings and
  ordinary progress through `stderr`.
- Runtime project/process creation with persistent configuration.
- First-class `td`, `pull`, `push`, `cb`, and `cm` workflows.

## v0.2.5 changes

- New red visual identity and initial home screen with original SuperCLI ASCII
  art.
- New **What's New** screen, accessible with `u` from the home screen.
- Hyprland-inspired tiled workspace: separated project, output/terminal and
  session tiles with gaps and a red focused border.
- Embedded interactive terminal using PTY/ConPTY plus VT emulation. PowerShell,
  SSH and interactive actions stay inside the SuperCLI layout.
- Safe removal of processes and workspaces from the project manager. Source
  directories and files are never touched.

## v0.2.7 changes

- Unified the application, tile and embedded-terminal backgrounds to remove
  darker gaps caused by nested ANSI style resets.
- Simplified the home screen by removing the project-manager shortcut and
  internal architecture terminology.
- Project management remains available with `n` inside the workstation.

## v0.2.8 changes

- `a` starts every stopped process in the selected workspace.
- Processes already running are preserved, and one startup failure does not
  prevent the remaining processes from being attempted.

## v0.2.9 changes

- Shows the Git branch for the selected process in the context line and session
  inspector when its directory belongs to a Git worktree.
- Ignores personal configuration, environment files, captures and logs so
  company-specific workstation data is not added to public commits.
- The installer now seeds new users from the neutral bundled configuration.

## v0.3.0 changes

- Added clipboard paste support to actions, forms and the embedded terminal.
- Fixed Shift/Caps Lock text and added `F6` as a reliable terminal detach key.
- Rebuilt `cb` around the original Profile workflow: confirm, carry or stash,
  create, publish, optionally commit tagged changes, and push again.
- Added checkout with a carry-or-stash working-tree policy.
- Limited log reads to the visible viewport and made YAML persistence atomic.
- Refined tile focus, status hierarchy and release branding without introducing
  mixed background surfaces.

## v0.3.1-alpha changes

- Finished `pull all` with repository discovery, deduplication, current-branch
  updates, concurrent execution and partial-failure reporting.
- Added the portable community plugin API v1, discovery host, example plugin,
  contributor/security guidance, source-available license and cross-platform CI.
- Added Windows capture opening, confirmed log clearing and retained-memory
  release.
- Added terminal cursor, modified key sequences and `F7` fullscreen mode.
- Added independent output fullscreen with `F7`/`Esc`, preserving log workflows.
- Fixed viewport-aware scrolling, CI push triggers and installer failure handling.
- Reworked Workspaces and Session panels while keeping one uniform background.
- Aligned the Go module with `github.com/2pedrohenrique/SuperCLI` and removed the
  previous personal surname from tracked source.

## License

SuperCLI is source-available under the MIT License with the Commons Clause v1.0.
You may use, inspect, modify, contribute and redistribute it without charge,
including inside a developer workstation. You may not sell SuperCLI, a renamed
copy, hosted access, or a product/service whose value derives entirely or
substantially from SuperCLI. See [`LICENSE`](LICENSE) for the governing terms.
Because the Commons Clause restricts commercial sale, this is a source-available
license rather than an OSI-approved open-source license.
