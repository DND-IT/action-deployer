# action-deployer

A GitHub Action that updates YAML values files and either commits+pushes or opens
a PR. Two modes:

- **Direct mode** — update one or more files with one value. No config file.
- **Matrix mode** — update values files across environments driven by
  `matrix.config.yaml`. One atomic commit for `auto` envs, one PR per `pr` env.

Built for Helm chart deploys (`image.tag`), but works on any YAML value.

## Quick start

### Direct mode — single file, auto-commit

```yaml
- uses: DND-IT/action-deployer@v1
  with:
    file: charts/my-app/values.yaml
    value: ${{ steps.build.outputs.tag }}
    token: ${{ secrets.GITHUB_TOKEN }}
```

### Direct mode — multiple files, one commit

```yaml
- uses: DND-IT/action-deployer@v1
  with:
    file: |
      charts/my-app/envs/dev/values.yaml
      charts/my-app/envs/staging/values.yaml
      charts/my-app/envs/prod/values.yaml
    value: "2.0.0"
    token: ${{ secrets.GITHUB_TOKEN }}
```

One commit. All three files updated atomically — if any file has no `image.tag`
block, nothing is written (pre-flight check).

### Direct mode — PR with auto-merge

```yaml
- uses: DND-IT/action-deployer@v1
  with:
    file: config/app.yaml
    value: "v2.0.0"
    mode: key
    key: app.image.tag
    deploy: pr
    branch: update/app-v2
    auto_merge: true
    token: ${{ secrets.GITHUB_TOKEN }}
```

### Matrix mode

Service pipeline defers per-env deploy policy to `matrix.config.yaml`:

```yaml
- uses: DND-IT/action-deployer@v1
  with:
    service: my-service
    version: ${{ github.event.release.tag_name }}
    sha: ${{ github.sha }}
    token: ${{ secrets.GITHUB_TOKEN }}
```

## Modes

### Update modes (`mode` input)

| Mode | How it finds the target |
|---|---|
| `image` (default) | Any YAML mapping with both `repository:` and `tag:` keys. Updates `tag:`. Works for `image:`, `web.image:`, `worker.image:`, etc. |
| `key` | Dot-notation path (`app.image.tag`, `web.replicas`). Requires `key` input. |
| `marker` | Any scalar with `# x-yaml-update` line comment. Comment preserved. |

Formatting, comments, and indentation are preserved exactly — no YAML reserialise.

### Deploy modes (`deploy` input, direct mode only)

| Deploy | Behavior |
|---|---|
| `auto` (default) | Update files → one commit → push to default branch |
| `pr` | Checkout new branch → update files → force-push → open/update PR. Requires `branch` input. |

## Inputs

### Direct mode (set `file` to activate)

| Input | Required | Default | Description |
|---|---|---|---|
| `file` | ✓ | — | Newline-separated file paths. Presence triggers direct mode. |
| `value` | ✓ | — | New scalar value. Applied to every file. |
| `mode` | | `image` | `image` \| `key` \| `marker` |
| `key` | if `mode=key` | — | Dot-notation path, e.g. `app.image.tag` |
| `deploy` | | `auto` | `auto` \| `pr` |
| `branch` | if `deploy=pr` | — | Deploy branch name, e.g. `update/foo` |
| `commit_message` | | `update N files: <value>` | Override the commit message |
| `auto_merge` | | `false` | Enable GitHub auto-merge on the PR |
| `merge_method` | | `SQUASH` | `MERGE` \| `SQUASH` \| `REBASE` |

### Matrix mode (omit `file`)

| Input | Required | Default | Description |
|---|---|---|---|
| `service` | ✓ | — | Service key in `matrix.config.yaml` |
| `version` | ✓ | — | Release version (used when env has `tag: version`) |
| `sha` | ✓ | — | Short commit SHA (used when env has `tag: sha`) |
| `config` | | `.github/matrix.config.yaml` | Path to matrix config |
| `charts_dir` | | `deploy/charts` | Root of per-service Helm values |

### Shared

| Input | Required | Default | Description |
|---|---|---|---|
| `token` | ✓ | — | GitHub token with `contents:write` + `pull-requests:write` |
| `git_user_name` | | `github-actions[bot]` | Commit author name |
| `git_user_email` | | `github-actions[bot]@users.noreply.github.com` | Commit author email |
| `dry_run` | | `false` | Print plan without writing |

## Outputs

| Output | Description |
|---|---|
| `deployed` | `true` if at least one file was updated |
| `environments` | JSON array of env names (matrix mode) |
| `commit_sha` | SHA of the deploy commit (auto mode) |
| `pr_urls` | JSON array of PR URLs opened/updated |
| `changed_files` | Newline-separated list of updated files |
| `diff` | `file: old → new` per line |

## `matrix.config.yaml` contract

Merge precedence (lowest → highest): **global < environment < service**.

```yaml
global:
  charts_dir: deploy/charts
  aws_region: eu-central-1

environment:
  dev:
    deploy: auto       # direct commit to default branch
    tag: version       # image tag = inputs.version
  staging:
    deploy: auto
    tag: version
  prod:
    deploy: pr         # open a PR instead
    tag: version
    auto_merge: false

service:
  my-service:
    ecr_repository: my-service
    # Optional service-level overrides:
    # deploy: auto
    # values_mode: key
    # values_key: web.image.tag
```

For each environment, action-deployer resolves to a per-env values file:
`<charts_dir>/<service>/envs/<env>/values.yaml`. All `auto` environments go in
one atomic commit; each `pr` environment gets its own branch (`deploy/<service>/<env>`)
and PR.

## How it updates YAML

Hybrid strategy: `yaml.v3` parses the document into a node tree to locate the
target line number, then a byte-level line replacement writes the new value.
Result: full structural awareness (key paths, nested mappings, marker comments)
with zero formatting drift.

## Local development

```bash
go build ./...
go test -race -count=1 ./...
```

Smoke-test a dry run against a local repo:

```bash
INPUT_FILE=$'dev/values.yaml\nprod/values.yaml' \
INPUT_VALUE=2.0.0 \
INPUT_TOKEN=x \
INPUT_DRY_RUN=true \
  go run ./cmd/action-deployer
```

## Permissions

The token passed via `token:` needs:
- `contents: write` — to push commits / deploy branches
- `pull-requests: write` — for `deploy: pr` environments

In a workflow:

```yaml
permissions:
  contents: write
  pull-requests: write
```

## See also

- `SPEC.md` — full design spec
- `docs/designs/action-deployer.md` — implementation plan with review decisions
- `TODO.md` — deferred work (Kustomize support, multi-repo gitops, rate-limit retry)
