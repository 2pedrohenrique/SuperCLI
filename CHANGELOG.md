# Changelog

## 0.3.1-alpha

- Prepared the repository for public collaboration with sanitized-history tests,
  strict YAML parsing, pinned CI actions, dependency automation, issue forms and
  portable starter configuration.
- Fixed a PTY shutdown race that could discard the final output of short-lived
  terminal commands on slower systems.
- Added output fullscreen with F7/Esc, preserving selection and log workflows.
- Matched scrolling to the visible log area without copying the entire buffer.
- Fixed CI push triggers for master and main and enabled manual runs.
- Made Windows installation stop on test/build failure before replacing binaries.
- Finished workspace-wide Pull All with unique-repository discovery,
  current-branch pulls, concurrency and partial-failure summaries.
- Added community plugin API v1, manifest discovery, namespaced action
  contributions and an example plugin.
- Added Windows Notepad opening after capture and confirmed output clearing.
- Added a visible terminal cursor, reliable control/navigation sequences and
  F7 fullscreen terminal mode.
- Added the bundled Git Insights plugin as a minimal community example.
- Replaced plain MIT licensing with MIT plus Commons Clause v1.0 to prohibit
  selling SuperCLI while preserving free use, modification and distribution.
- Reworked Workspaces and Session panels with a uniform background surface.
- Added public contribution, security, conduct, license and CI files.
- Renamed the Go module to `github.com/2pedrohenrique/SuperCLI`.

## 0.3.0

- Added clipboard paste throughout forms, actions and the embedded PTY.
- Fixed uppercase terminal input and added reliable F6/Ctrl+] detach handling.
- Expanded `cb` to publish branches and optionally commit/push carried changes.
- Added checkout with carry-or-stash behavior.
- Added viewport-only log reads and atomic configuration writes.
- Refined the tiled UI while preserving a single consistent background.

## 0.2.9

- Added asynchronous Git branch context for the selected process.
- Excluded personal configuration and runtime files from version control.
- Changed the installer to use the neutral bundled configuration for new users.

## 0.2.8

- Added a visible `a` shortcut to start every process in the selected workspace.
- Workspace startup skips active processes and continues after individual
  startup failures, returning all failures together.

## 0.2.7

- Unified tile, application and VT-emulator background colors to eliminate
  darker rectangles around styled text and terminal output.
- Simplified the home screen and kept project management inside the workstation.
- Removed internal architecture terminology from the visible product identity.

## 0.2.5

- Replaced the purple palette with a red visual identity.
- Added an initial home screen with original SuperCLI ASCII art.
- Added an in-app What's New screen.
- Reworked the workstation into Hyprland-inspired focused tiles.
- Added PTY/ConPTY-backed embedded terminal sessions with VT emulation.
- Interactive actions such as SSH now remain inside SuperCLI.
- Added safe persistent removal of processes and workspaces.
- Added automated PTY output/input and tile dimension tests.

## 0.2.0

- Rebuilt the TUI with a responsive workstation layout and centered modal views.
- Fixed semantic severity parsing for tools that use stderr for non-error output.
- Added explicit stopped process state and reliable status feedback.
- Added persistent project and process creation from inside the application.
- Added native Git pull, push, feature/hotfix branch (`cb`) and tagged commit
  (`cm`) workflows.
- Added project-directory interactive shell (`td`).
- Added tests for UI bounds, Windows argument paths, config persistence, Git
  workflows, and user-requested process stops.

## 0.1.0

- Initial microkernel, concurrent process supervisor, log capture, notifications,
  configurable actions and Bubble Tea interface.
