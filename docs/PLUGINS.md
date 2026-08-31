# Community plugins

SuperCLI plugins are deliberately **out of process**. A plugin is a directory
containing a versioned `plugin.yaml` and, optionally, scripts or executables.
This model works on Windows, Linux and macOS; Go's native `plugin` package does
not support Windows and would couple extensions to the exact host build.

## Directory layout

```text
plugins/
└── my-plugin/
    ├── plugin.yaml
    ├── bin/
    │   ├── my-plugin.exe        # Windows, optional
    │   └── my-plugin            # Unix, optional
    └── README.md
```

The directory name must equal the manifest `id`. Installed builds use
`%LOCALAPPDATA%\SuperCLI\plugins` on Windows. `SUPERCLI_PLUGIN_DIR` or
`--plugin-dir` can select another directory. Diagnose discovery with:

```powershell
supercli --list-plugins
```

## API v1 manifest

```yaml
api_version: supercli.dev/v1
id: docker-tools
name: Docker Tools
version: 0.1.0
description: Docker workflows for local development
homepage: https://example.com/docker-tools
actions:
  - id: compose-up
    name: Start Compose stack
    command: docker
    args: [compose, up, -d]
    dir: "{{workspace_dir}}"
    confirm: true
    interactive: false
    workspaces: ["*"]
```

An empty `workspaces` list, or `"*"`, exposes an action in every workspace.
Explicit IDs restrict it. The host namespaces action IDs as
`plugin-id--action-id`, so plugin actions cannot silently replace user actions.

Two host tokens are resolved before an action is displayed:

- `{{plugin_dir}}`: absolute directory containing `plugin.yaml`.
- `{{workspace_dir}}`: directory of the workspace's first process.

Action inputs keep the same safe `{{input_id}}` argument substitution used by
built-in configurable actions. Commands are executed directly, without an
implicit shell. Relative commands such as `./bin/my-plugin` are resolved inside
the plugin directory; `.exe` is added on Windows when no extension is present.

The public Go types live in `pkg/pluginapi`. Authors do not need Go: an action
may launch any local executable or interpreter. `plugins/git-insights` is the
small manifest-only plugin installed with SuperCLI; `examples/plugins/go-toolchain`
shows another portable example.

## Security boundary

A plugin can run commands with the current user's permissions. SuperCLI
validates manifests and relative paths, but it cannot make arbitrary executable
code trustworthy. Review source and releases before installation. The host does
not download, update or execute plugins automatically at startup; actions run
only when selected, with confirmation when requested by the manifest.

## Compatibility

`api_version` is the compatibility boundary. Additive fields may appear within
v1; breaking manifest behavior requires v2. Unknown YAML fields are rejected so
spelling errors fail clearly instead of producing surprising commands.
