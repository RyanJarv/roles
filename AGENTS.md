# Repository Guidelines

## Project Structure & Module Organization
`main.go` is the CLI entrypoint. Core code lives under `pkg/`: `pkg/cmd` handles CLI actions such as setup, cleanup, and scan execution; `pkg/scanner` contains the scan pipeline and storage logic; `pkg/plugins` holds AWS service probes; `pkg/utils` and `pkg/arn` provide shared helpers. Helper scripts for account and principal list generation live in `scripts/`. Compiled binaries are written to `build/` by the Makefile.

## Build, Test, and Development Commands
Use `make build` to produce the default binaries in `build/darwin-arm/roles` and `build/linux-arm/roles`. Use `go test ./...` for the full test suite across all packages. Run the CLI locally with `go run . -help`, `go run . -profile scanner -account-list ./accounts.list -roles ./roles.list`, or `go run . -profile scanner -account-list ./accounts.list -principals ./principals.list`. Use `go test ./pkg/scanner -run TestScanWithPlugins` when iterating on scanner behavior.

## Coding Style & Naming Conventions
Follow standard Go formatting: tabs for indentation, `gofmt` for layout, and grouped imports. Keep packages focused and small; new AWS probes should follow the existing plugin shape in `pkg/plugins` with clear `Setup`, `ScanArn`, and `CleanUp` behavior. Use exported CamelCase names only when cross-package access is required; keep internal helpers lowercase. Preserve the input contract: `-roles` takes bare role names, while `-principals` takes explicit `role/...` or `user/...` entries.

## Testing Guidelines
Write table-driven tests where inputs vary, and keep tests next to the package they cover as `*_test.go`. Current coverage centers on `pkg/scanner`, `pkg/plugins`, `pkg/cmd`, and `pkg/utils`; extend those patterns instead of creating ad hoc harnesses. Run `go test ./...` before opening a PR. Add focused regression tests for concurrency, retry, and region-specific plugin behavior when fixing scanner or plugin bugs.

## Commit & Pull Request Guidelines
Recent history uses short imperative subjects with prefixes like `fix:` and `docs:`. Keep commit messages specific, for example `fix: retry errored ARNs without deadlock`. PRs should explain behavioral impact, call out any AWS permissions or scanning changes, and link the related issue if one exists. Include command output or sample CLI usage when changing operator-facing behavior.
