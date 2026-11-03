# Session Lifecycle

## Create Or Extend

| Source | Command | Result |
| --- | --- | --- |
| Current repo | `autofeat new FEATURE` | Git worktree |
| Other local repo | `autofeat new FEATURE PATH` (any dir inside the repo) | Git worktree |
| Remote URL | `autofeat new FEATURE URL` (`http://`, `https://`, `git@`) | Fresh clone |
| Template | `--template NAME` | All template repos; feature must be new |
| Azure DevOps build | `--azure-build URL\|ID` | Clone per build repo, pinned to the build's commit; feature must be new |

- Re-running `new` with an existing feature attaches another repo.
- Feature branch: reuse local branch, else cached `origin/FEATURE`, else
  create from base. `new` never fetches.
- Base ref order: `--ref` > remembered ref for that repo > detected
  `master`/`main` > `default_base_branch`. `--ref` accepts branches, tags,
  SHAs, full refs, and revision expressions, and is remembered per repo.
- Sources are auto-detected: URL prefixes mean remote, anything else is a
  local path. There are no `--local`/`--remote` flags.
- `--template`, `--azure-build`, and `--ref` are mutually exclusive, and
  `--template`/`--azure-build` take no positional source.
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
  repo. The PAT is used for REST calls only; a PAT used as an HTTPS Git
  password needs Code (Read).
- Clones the primary repo plus Git repository resources (Azure Repos, GitHub,
  Bitbucket). Pipeline artifacts and non-Git resources are out of scope. Failures leave
  no partial session.

## Inspect

- `autofeat list`: sessions, most recent `LAST ACTIVITY` first, with cached
  `DRIFT`. Never touches the network.
- `autofeat status [SELECTOR...]`: per-repo `BRANCH`, `WORKTREE`, `BASE`,
  `DRIFT` (vs. cached base), `PUSH` (vs. cached origin), and `STATE`. Defaults to
  all sessions. Exits 0 regardless of repo health.
- `STATE` values: `ready`, `dirty`, `behind-base`, `unpushed`, `remote-ahead`,
  `remote-diverged`, `rebasing`, `wrong-branch`, `detached`, `missing`, `error`.
- `list` includes an `ARTIFACTS` count; `status` appends a
  `FEATURE TYPE ID REPO CREATED VALUE` table when sessions have artifacts.
- `autofeat dash` is an interactive terminal UI for humans and fails without a
  TTY. Do not run it; use `list` and `status` instead.

## Open

```sh
autofeat open FEATURE                                  # editor_cmd on .code-workspace
autofeat open FEATURE --copilot                        # interactive headless_cmd (blocks)
autofeat open FEATURE --devcontainer ./x/.devcontainer/devcontainer.json
```

- `--copilot` and `--devcontainer` are mutually exclusive. `--copilot` runs
  `headless_cmd` in the current terminal with the feature directory as its
  root and waits for it to exit.
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
- Every origin-backed base is fetched, with retries, before any rebase, so a
  network failure leaves the session untouched. Repos without `origin` use the
  local base. Tags and SHAs are resolved locally.
- If a rebase conflicts or fails, sync attempts to abort it and restore repos
  already rebased to their pre-sync commits. Only `synchronization rolled back`
  confirms a complete rollback. If the error says `could not be fully rolled
  back`, inspect the repository errors and `autofeat status FEATURE` before
  retrying. See
  [Safety And Recovery](./safety-and-recovery.md#sync-conflicts-and-rollback).
- `--keep-conflicts` instead stops at the conflict with the rebase in progress.
- Only the feature branch is rewritten. `rerere` is enabled unless configured,
  so recorded resolutions are replayed and the rebase continues. Commits
  already in the base are dropped.
- Every selected session is synced even if one fails. With several sessions,
  a `Synchronized N of M sessions` summary is printed; any failure exits
  non-zero.

## Artifacts

```sh
autofeat artifact add FEATURE "https://dev.azure.com/org/proj/_build/results?buildId=123"
autofeat artifact add FEATURE AB#4567 --repo api     # work item; !45 is a pull request
autofeat artifact add FEATURE 98765 --type ado-build  # plain ids need --type
autofeat artifact list FEATURE --json
autofeat artifact remove FEATURE 123 --type ado-build # reference, equivalent URL, or id
```

- Types are inferred offline: `ado-build`, `ado-work-item`, `ado-pull-request`,
  `ado-commit`, `ado-branch`, `ado-repo`, `azure-resource` (ARM id or portal
  URL), `github-pull-request`, `github-issue`, `github-commit`, `github-run`,
  and `link` for any other URL.
- Re-adding an equivalent reference is a no-op. `--type` errors if it
  contradicts an inferred type (`link` always works).
- Plain ids are distinct per type and `--repo`. `artifact remove` errors when a
  reference matches several artifacts; narrow it with `--type` or `--repo`.
- `--repo` takes an attached worktree name; removing that repo clears the link.
- `teardown` prints the session's artifacts after removing it.
