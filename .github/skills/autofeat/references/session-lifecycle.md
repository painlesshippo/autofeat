# Session Lifecycle

## Create Or Extend

| Source | Command | Result |
| --- | --- | --- |
| Current repo | `autofeat new FEATURE` | Git worktree |
| Other local repo | `--local PATH` (any dir inside the repo) | Git worktree |
| Remote URL | `--remote URL` (HTTP(S) or SSH) | Fresh clone |
| Template | `--template NAME` | All template repos; feature must be new |
| Azure DevOps build | `--azure-build URL\|ID` | Clone per build repo, pinned to the build's commit |

- Re-running `new` with an existing feature attaches another repo.
- Feature branch: reuse local branch, else cached `origin/FEATURE`, else
  create from base. `new` never fetches.
- Base ref order: `--ref` > remembered ref for that repo > detected
  `master`/`main` > `default_base_branch`. `--ref` accepts branches, tags,
  SHAs, full refs, and revision expressions, and is remembered per repo.
- `--template` cannot be combined with `--ref`, `--local`, or `--remote`.
- Directory names are flattened (`feature/x` -> `feature-x`). Colliding names
  are rejected; choose another feature name.

### Azure DevOps

```sh
autofeat new bug/repro --azure-build "https://dev.azure.com/ORG/PROJ/_build/results?buildId=4821"
autofeat new bug/repro --azure-build 4821 --azure-org ORG --azure-project PROJ
# On-prem: --azure-org DefaultCollection --azure-url https://server/tfs
```

- Requires `AZURE_DEVOPS_EXT_PAT` or `AZURE_DEVOPS_PAT` with Build (Read).
- Cloning uses normal Git credentials (helper/SSH) with read access to every
  repo. The PAT is used for REST calls only.
- Clones the primary repo plus Git repository resources (Azure Repos, GitHub,
  Bitbucket). Artifacts and non-Git resources are out of scope. Failures leave
  no partial session.

## Inspect

- `autofeat list`: sessions, most recent `LAST ACTIVITY` first, with cached
  `DRIFT`. Never touches the network.
- `autofeat status [SELECTOR...]`: per-repo `BRANCH`, `WORKTREE`, `BASE`,
  `DRIFT` (vs. cached base), `PUSH` (vs. cached origin), and `STATE`. Defaults to
  all sessions. Exits 0 regardless of repo health.
- `STATE` values: `ready`, `dirty`, `behind-base`, `unpushed`, `remote-ahead`,
  `remote-diverged`, `rebasing`, `wrong-branch`, `detached`, `missing`, `error`.

## Open

```sh
autofeat open FEATURE                                  # editor_cmd on .code-workspace
autofeat open FEATURE --copilot                        # interactive headless_cmd
autofeat open FEATURE --devcontainer ./x/.devcontainer/devcontainer.json
```

- `--devcontainer` is required to be an explicit, readable file path (relative
  to cwd). Autofeat passes it through untouched. The config must mount worktree
  Git metadata if Git is needed inside the container. Requires a VS Code-family
  `editor_cmd` that supports `--file-uri`.

## Sync

```sh
autofeat status FEATURE
autofeat sync FEATURE
```

- Preconditions are checked for every repo before any work starts: repo
  present, on the feature branch, clean (including untracked files), and no
  rebase in progress.
- Origin branches are fetched. Repos without `origin` use the local base.
  Tags and SHAs are resolved locally.
- On conflict, sync stops and leaves the rebase in progress. See
  [Safety And Recovery](./safety-and-recovery.md#rebase-recovery).
