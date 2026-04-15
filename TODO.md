# action-deployer — TODO

## Spec

See SPEC.md for the full design and `docs/designs/action-deployer.md` for the
implementation plan with eng-review decisions.

## Implementation Checklist

### Core
- [x] `internal/config` — parse `matrix.config.yaml` (merge precedence), new fields (values_mode, values_key, auto_merge, merge_method), deterministic sort
- [x] `internal/values` — hybrid yaml.v3 approach: unified `SetTag`/`ReadTag` with `UpdateOptions{Mode, Key}`. Image/key/marker modes.
- [x] `internal/git` — Configure, Add, Commit, Push (with auth-failure detection), CheckoutBranch, ForcePush, RevParse, DefaultBranch
- [x] `internal/github` — PullRequest with NodeID, NewClientWithBase, EnsurePR returns `*PullRequest`, error-body decoding, 30s timeout, EnableAutoMerge (GraphQL)
- [x] `internal/deployer` — full deployAuto + deployPR with atomic commit / PR branch flow, valuesPath containment, rich outputs, Step Summary

### Action wiring
- [x] `cmd/action-deployer/main.go` — inputs, run_id correlation logging, writes outputs + step summary, splitLines helper
- [x] `action.yaml` — inputs, outputs (deployed, environments, commit_sha, pr_urls, changed_files, diff)
- [x] `Dockerfile` — golang:1.25-alpine → alpine:3.21 with git + ca-certificates

### Quality
- [x] 55 tests across all packages
- [x] Unit tests: config merge precedence, values (image/key/marker modes), git operations, github REST+GraphQL
- [x] Integration tests: full auto-deploy + full PR-deploy flows against real git + httptest
- [x] dry-run mode — exercised in both `TestDeployAuto_DryRun` and `TestDeployPR_DryRun`
- [x] Structured logging via `log/slog` with `run_id` attribute

### CI
- [x] `.github/workflows/test.yaml` — lint + test + PR docker build + PR image cleanup (DND-IT convention)
- [x] `.github/workflows/release.yaml` — release-please + GHCR build/push + v{major}/v{major.minor} alias tags

### Release infrastructure (DND-IT convention)
- [x] `catalog-info.yaml` — Backstage catalog entry
- [x] `release-please-config.json` — release-please config (release-type: go)
- [x] `.release-please-manifest.json` — starts at 0.1.0
- [x] `LICENSE` — MIT

### Docs
- [x] `README.md` — direct + matrix usage, inputs/outputs tables, matrix.config.yaml contract

## Deferred (from /plan-ceo-review 2026-04-14)

### P2: GitHub API 429 rate limit retry
**What:** In `github.go:do()`, on HTTP 429, read `Retry-After` header (or default 60s), sleep, retry once.
**Why:** Prevents silent mid-run failures when a service has many PR environments in one run. GitHub always sets `Retry-After` on 429 responses.
**How to start:** `github.go:do()` — add a case for `resp.StatusCode == 429` before the `>= 400` error check.
**Effort:** XS with CC+gstack | **Depends on:** None

### P2: Multi-repo gitops support
**What:** Add optional `gitops_repo` input. When set, the action clones that repo (using the provided token), updates values files there, commits, pushes, and opens PRs against the gitops repo's default branch.
**Why:** DND-IT teams that centralize Helm charts in a dedicated gitops repo cannot use action-deployer today.
**How to start:** Add `GitopsRepo string` to `deployer.Options`. In `deployAuto`/`deployPR`, if non-empty: `git clone https://x-access-token:{token}@github.com/{gitops_repo} /tmp/gitops` into a temp dir and use that as `WorkDir`.
**Effort:** M with CC+gstack | **Depends on:** None

### P2: Kustomize support
**What:** In image mode, detect Kustomize `images:` list format (`name`/`newTag` pairs) in addition to Helm `repository`/`tag` pairs. Update `newTag` when `name` ends with the service image name.
**Why:** DND-IT teams using Kustomize overlays cannot use action-deployer today.
**How to start:** In `values.go` `findImageTags`: add a case for YAML sequences where sibling key is `name` (instead of `repository`) and the update target is `newTag` (instead of `tag`).
**Effort:** S with CC+gstack | **Depends on:** None (hybrid yaml.v3 is already implemented)

### P2: version_strategy field wiring
**What:** `ServiceConfig.VersionStrategy` is parsed but ignored. Propagate to `Environment.VersionStrategy` and switch on it in `resolveTag()`.
**Why:** Wiring it now prevents a future breaking config change.
**How to start:** `config.go` — add `VersionStrategy string` to `Environment`; propagate in `Resolve()`. `deployer.go:resolveTag()` — switch on it.
**Effort:** XS with CC+gstack | **Depends on:** None

