# action-deployer — TODO

## Spec

See SPEC.md for the full design.

## Implementation Checklist

### Core
- [ ] `internal/config` — parse `matrix.config.yaml` (global + environment + service merging, same logic as action-config)
- [ ] `internal/values` — locate and update `image.tag` in a Helm values YAML file (web / worker chart agnostic)
- [ ] `internal/git` — `clone → stage → commit → push` with exponential-backoff retry on push conflict (pull --rebase + retry)
- [ ] `internal/github` — create / update pull request via GitHub REST API (for `deploy: pr` environments)
- [ ] `internal/deployer` — orchestrate: read config → split auto vs PR environments → update files → atomic commit OR create PRs

### Action wiring
- [ ] `main.go` — parse GitHub Actions inputs, call deployer, write outputs
- [ ] `action.yml` — declare inputs, outputs, Docker entrypoint
- [ ] `Dockerfile` — multi-stage build (golang:1.24-alpine → scratch/alpine)

### Quality
- [ ] Unit tests for config parsing (merge precedence: global < env < service)
- [ ] Unit tests for values file update (web chart, worker chart, missing key)
- [ ] Integration test: full deploy run against a fixture repo (use `go-git` in-memory or temp dir)
- [ ] `dry-run` mode — print plan, no git push, no PR creation
- [ ] Structured JSON logging via `log/slog`

### CI
- [ ] `.github/workflows/ci.yaml` — lint (golangci-lint) + test on PR
- [ ] `.github/workflows/release.yaml` — action-releaser on merge to main, build+push Docker image to GHCR

### Docs
- [ ] `README.md` — usage example, all inputs/outputs table, matrix.config.yaml contract
