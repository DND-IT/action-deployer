# action-deployer

A GitHub Action (Go) that updates YAML values files and either auto-commits or opens PRs.
Two modes:
- **Matrix mode** — driven by `matrix.config.yaml`; resolves per-env deploy policy
- **Direct mode** — one or more files updated with one value, no config file required

## Repo Structure

Follows the DND-IT action convention (see sibling repos: `action-config`, `action-releaser`):

```
cmd/action-deployer/main.go   — entry point; reads INPUT_* env vars
action.yaml                   — GitHub Action definition (not .yml)
Dockerfile                    — multi-stage build (golang:1.25-alpine → alpine + git)
catalog-info.yaml             — Backstage catalog entry
release-please-config.json    — release-please (release-type: go)
.release-please-manifest.json — release-please version manifest
LICENSE                       — MIT
internal/
  config/config.go            — parse matrix.config.yaml; Resolve() merges global < env < service
  values/values.go            — update scalars in YAML via hybrid yaml.v3 + byte-level replacement
  git/git.go                  — configure/add/commit/push/checkout/force-push
  github/github.go            — GitHub REST (PR CRUD) + GraphQL (enablePullRequestAutoMerge)
  deployer/deployer.go        — orchestrates both matrix + direct flows
.github/workflows/
  test.yaml                   — lint + test + PR Docker build + PR image cleanup
  release.yaml                — release-please + GHCR push + v{major}/v{major.minor} alias tags
SPEC.md                       — full design spec
TODO.md                       — implementation checklist
docs/designs/                 — design docs (CEO plan + eng review)
```

## Key Design Decisions

- **Hybrid YAML editing** — `values.go` parses with `yaml.v3` to locate the target node's line
  number, then does a byte-level line replacement. Preserves comments, indentation, inline
  comments exactly. Supports `image` mode (auto-detect mapping with `repository`+`tag`), `key`
  mode (dot-notation), `marker` mode (`# x-yaml-update`).
- **Atomic multi-file writes** — direct mode pre-flights every file (`values.HasTarget`) before
  any write. Prevents partial updates when one file is misconfigured.
- **Atomic commit** — `deploy: auto` environments stage all files before a single `git commit+push`.
  Exponential-backoff retry (pull --rebase + push) handles concurrent pushes.
- **Single runtime dependency** — only `gopkg.in/yaml.v3`.

## matrix.config.yaml Contract

Merge precedence (lowest → highest): `global` < `environment` < `service`

```yaml
environment:
  dev:
    deploy: auto   # direct commit to default branch
    tag: version   # image tag = version input; alternative: sha
  prod:
    deploy: pr     # open a pull request

service:
  my-service:
    deploy: auto   # overrides environment-level deploy for all envs
```

See `README.md` for the full contract.

## Running Locally

```bash
go build ./...
go test -race -count=1 ./...

# Matrix mode
INPUT_SERVICE=ts-spa INPUT_VERSION=2026.04.14.5 INPUT_SHA=abc1234 \
  INPUT_TOKEN=x INPUT_DRY_RUN=true \
  go run ./cmd/action-deployer

# Direct mode
INPUT_FILE=charts/my-app/values.yaml INPUT_VALUE=2.0.0 \
  INPUT_TOKEN=x INPUT_DRY_RUN=true \
  go run ./cmd/action-deployer
```

## Status

Core + tests complete (77 passing). See TODO.md for deferred items (Kustomize support,
multi-repo gitops, rate-limit retry, version_strategy wiring).
