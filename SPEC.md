# action-deployer — Spec

## Problem

In a multi-environment CI pipeline, each environment's deploy job independently does
`git pull → commit → push` to update a Helm values file. When multiple environments
are `deploy: auto`, these jobs race and the second push is rejected.

## Solution

A single GitHub Action that reads the matrix config, resolves all environments for a
service, and handles all deploy-mode routing internally:

- **`deploy: auto`** environments → values files updated atomically in one git commit+push
- **`deploy: pr`** environments → a GitHub PR is opened (or updated) on a deploy branch

The caller pipeline collapses to one step with no conditional logic.

## Usage

```yaml
- uses: DND-IT/action-deployer@v0
  with:
    service: go-service
    version: ${{ steps.releaser.outputs.version }}
    sha: ${{ steps.build.outputs.sha }}
    token: ${{ steps.app-token.outputs.token }}
```

## Inputs

| Input            | Required | Default                         | Description |
|------------------|----------|---------------------------------|-------------|
| `service`        | yes      |                                 | Service name; must match a key under `service:` in the matrix config |
| `version`        | yes      |                                 | Release version string (e.g. `1.4.0`, `2026.04.14`) |
| `sha`            | yes      |                                 | Short commit SHA used when `tag: sha` |
| `token`          | yes      |                                 | GitHub token with `contents: write` and `pull-requests: write` |
| `config`         | no       | `.github/matrix.config.yaml`    | Path to the matrix config file |
| `charts_dir`     | no       | `deploy/charts`                 | Root directory for per-service Helm chart overrides |
| `git_user_name`  | no       | `github-actions[bot]`           | Git commit author name |
| `git_user_email` | no       | `github-actions[bot]@users.noreply.github.com` | Git commit author email |
| `dry_run`        | no       | `false`                         | Print plan without pushing or creating PRs |

## Outputs

| Output        | Description |
|---------------|-------------|
| `deployed`    | `true` if at least one environment was updated |
| `environments`| JSON array of environment names that were deployed |

## Matrix Config Contract

The action reads `matrix.config.yaml` with this structure (same as `action-config`):

```yaml
global:
  charts_dir: deploy/charts       # overridden by input

environment:
  dev:
    aws_account_id: "..."
    deploy: auto                  # direct commit to default branch
    tag: version                  # image tag = version; alternative: sha
  prod:
    aws_account_id: "..."
    deploy: pr                    # open a pull request
    tag: version

service:
  go-service:
    ecr_repository: fission-demo-go
    # service-level deploy/tag overrides environment-level
```

Merge precedence (lowest → highest): `global` < `environment` < `service`

## Behavior

1. Parse `matrix.config.yaml`. Build the list of environments for the given `service`,
   applying the merge precedence above.

2. Partition environments:
   - `auto` — environments where resolved `deploy == "auto"`
   - `pr`   — environments where resolved `deploy == "pr"`

3. **Auto environments** (atomic):
   - For each environment determine the image tag:
     - `tag == "version"` → use `version` input
     - `tag == "sha"` → use `sha` input
   - Update `{charts_dir}/{service}/envs/{environment}/values.yaml`:
     - Find the `image:` block and set its `tag:` field
     - Works for both `web:` and `worker:` chart roots
   - Stage all changed files (`git add`)
   - One `git commit -m "deploy({service}): {version}"`
   - `git push` with exponential-backoff retry (pull --rebase + push, up to 3 attempts)
     to handle concurrent pushes from other service pipelines

4. **PR environments**:
   - For each environment, create or update a PR:
     - Branch: `deploy/{service}/{environment}`
     - Title: `deploy({service}/{environment}): {version}`
     - Labels: `deploy`
   - Each PR is independent; no direct push to the default branch

5. **Dry-run mode**: Log every action that would be taken; exit 0 without mutating state.

## Values File Update

The action updates the `tag:` field within the first `image:` block found in the YAML
file. It does NOT do a full YAML parse-and-reserialize (which would destroy comments and
formatting); instead it uses a targeted line-oriented approach:

```
image:            ← anchor
  repository: …
  tag: OLD        ← replace this line's value
```

Both `web.image.tag` and `worker.image.tag` are handled automatically since the scan
is chart-root-agnostic.

## Error Handling

- Missing values file → hard error (misconfigured charts_dir)
- `tag:` field not found in values file → hard error
- Push conflict after max retries → hard error with clear message pointing to the
  concurrent-push root cause
- PR creation failure → hard error
