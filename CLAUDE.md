# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Go CLI tool for unauthenticated enumeration of AWS IAM principal ARNs. It probes whether specific IAM role or user ARNs exist in target AWS accounts by abusing resource policy validation on AWS services (SNS, SQS, S3, ECR Public, S3 Access Points). Each service acts as a "plugin" — it creates a resource, sets a policy referencing a candidate principal ARN, and checks whether AWS accepts or rejects the principal.

This tool is part of the larger `saas-map` project (see parent directory's CLAUDE.md) and provides the scanning engine that discovers which SaaS provider roles exist in a given AWS account.

## Commands

```bash
make build                    # Build for darwin-arm64 and linux-arm64 into build/
go test ./...                 # Run all tests
go test ./pkg/scanner/        # Run scanner tests
go test ./pkg/plugins/        # Run plugin tests
go test -run TestName ./pkg/  # Run a single test
```

### Running the scanner

```bash
./build/darwin-arm/roles -profile <aws-profile> -setup                    # One-time setup (creates probe resources)
./build/darwin-arm/roles -profile <aws-profile> -account-list accounts.list -roles roles.list  # Scan legacy role lists
./build/darwin-arm/roles -profile <aws-profile> -account-list accounts.list -principals principals.list  # Scan explicit role/... and user/... principals
./build/darwin-arm/roles -json -profile <aws-profile> -account-list accounts.list -principals principals.list  # JSONL output
./build/darwin-arm/roles -profile <aws-profile> -clean                    # Tear down probe resources
```

## Architecture

### Scanning Flow

1. **`main.go`** — CLI flag parsing, delegates to `pkg/cmd`
2. **`pkg/cmd/run.go`** — Orchestrates a scan: loads AWS configs across all org accounts/regions, builds principal ARN lists from templates, runs scanner, outputs results
3. **`pkg/arn/`** — Expands Go template principal names (`{{.AccountId}}`, `{{.Region}}`) into concrete ARNs for each account/region combination. `-roles` still prepends `role/`, while `-principals` expects explicit `role/...` or `user/...` entries.
4. **`pkg/scanner/main.go`** — Core scan loop. Two-phase approach: first scans "root ARNs" (`arn:aws:iam::<account>:root`) to check if accounts exist, then scans individual principal ARNs only for confirmed accounts. Uses token-bucket rate limiting and concurrent plugin goroutines
5. **`pkg/scanner/storage.go`** — JSON file cache at `~/.roles/<name>.json` with file locking. Caches ARN existence results to avoid rescanning. Use `-force` to bypass
6. **`pkg/plugins/`** — Each plugin implements the `Plugin` interface and uses a different AWS service to probe principal existence

### Plugin System

All plugins implement `plugins.Plugin` (in `pkg/plugins/types.go`):

```go
type Plugin interface {
    Name() string
    Setup(ctx *utils.Context) error       // Creates AWS resources (only with -setup flag)
    ScanArn(ctx *utils.Context, arn string) (bool, error)  // Probes if ARN exists
    CleanUp(ctx *utils.Context) error     // Tears down resources (only with -clean flag)
}
```

Plugins are registered in `pkg/cmd/main.go` via `LoadAllPlugins()`. Each plugin gets instantiated per-region with a configurable concurrency (thread count). The initializer must construct all resource ARNs deterministically — `Setup()` is only called once, not on every run.

Current plugins: ECR Public, S3 Access Points, S3 Buckets, SNS Topics, SQS Queues.

### Input Format

Account and principal lists are plain text files, one entry per line. Lines support `# comments` after the value. Templates use Go `text/template` syntax for parameterization. The `-roles` flag accepts bare role names and prepends `role/`; the `-principals` flag accepts explicit `role/...` or `user/...` entries. Both flags accept comma-separated paths, and each path can be a file or directory of `.list` files.

### Key Design Decisions

- **Root-first scanning**: Checks account root ARN before scanning individual principals, avoiding wasted API calls against nonexistent accounts
- **Plugin concurrency**: Each plugin instance runs in its own goroutine consuming from a shared input channel; the rate limiter is shared across all plugins
- **Results are yielded via `iter.Seq2`** (Go 1.23 range-over-func) — callers iterate results as they arrive rather than waiting for completion
