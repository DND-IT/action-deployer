# action-deployer

A GitHub Action (Go) that atomically deploys Helm chart value changes across environments,
driven by `matrix.config.yaml`. Replaces scattered conditional logic in service pipelines.

## Purpose

Reads `matrix.config.yaml`, resolves environments for a service, then:
- `deploy: auto` — updates all values files and pushes ONE atomic commit to the default branch
- `deploy: pr`   — creates or updates a PR on a `deploy/{service}/{env}` branch

## Repo Structure

```
main.go                      — entry point; reads INPUT_* env vars
action.yml                   — GitHub Action definition
Dockerfile                   — multi-stage build (golang:1.24-alpine → alpine + git)
internal/
  config/config.go           — parse matrix.config.yaml; Resolve() merges global < env < service
  values/values.go           — update image.tag in a Helm values file without full reserialise
  git/git.go                 — git configure / add / commit / push with retry
  github/github.go           — GitHub REST API: create/update deploy PRs
  deployer/deployer.go       — orchestrates the full flow
SPEC.md                      — full design: inputs, outputs, merge precedence, error handling
TODO.md                      — implementation checklist
```

## Key Design Decisions

- **No full YAML reserialise** — `values.go` uses a line-oriented scanner to update `tag:` inside
  the `image:` block. This preserves comments and formatting. Chart root (`web` / `worker`) is
  irrelevant; the scan is agnostic.
- **Atomic commit** — all `deploy: auto` environments are staged before a single `git commit + push`.
  Exponential-backoff retry (pull --rebase + push, up to 3 attempts) handles concurrent pushes
  from other service pipelines.
- **Single dependency** — only `gopkg.in/yaml.v3` for config parsing. No framework.

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

## Running Locally

```bash
go build ./...
go test ./...

# Simulate an action run
INPUT_SERVICE=ts-spa \
INPUT_VERSION=2026.04.14.5 \
INPUT_SHA=abc1234 \
INPUT_TOKEN=ghp_... \
INPUT_DRY_RUN=true \
  go run .
```

## What Needs Finishing (see TODO.md)

- `deployPR` in `deployer.go` has a stub — needs to push values change to the deploy branch
  before calling `EnsurePR`
- Tests for `config`, `values`, and an integration test for the full deploy flow
- CI workflows: lint + test on PR, release pipeline using action-releaser
