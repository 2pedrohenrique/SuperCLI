# Contributing to SuperCLI

Thank you for improving the developer workstation. Contributions can target the
core, documentation, platform adapters or community plugins.

## Development loop

1. Fork the repository and create a focused branch.
2. Write or update a test that demonstrates the behavior (Red).
3. Implement the smallest coherent change (Green).
4. Refactor names and boundaries while tests remain green.
5. Run the quality gates:

```powershell
go test ./...
go vet ./...
go build ./cmd/supercli
```

Keep platform-specific behavior behind build-tagged files and avoid adding
company names, hosts, paths, credentials or personal workstation configuration.
`supercli.yaml` is intentionally ignored; tests and examples must use neutral
temporary data.

## Pull requests

Describe the user problem, the chosen boundary and how it was tested. Keep pull
requests small enough to review. New plugin API behavior needs contract tests in
`pkg/pluginapi` or `internal/pluginhost` plus an update to `docs/PLUGINS.md`.

For security reports, follow `SECURITY.md` instead of opening a public issue.
