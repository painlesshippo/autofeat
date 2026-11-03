# Safety And Recovery

Always run `autofeat status FEATURE` before a destructive operation.

## Remove One Repository

```sh
autofeat remove FEATURE repo1                 # worktree directory name
autofeat remove FEATURE /path/inside/repo     # attached local repo
autofeat remove FEATURE URL                   # remote clone
```

- URLs match remote clones. A directory inside an attached local repo selects
  it. Otherwise `REPO` matches the worktree directory first, then the
  repository name. It fails if several repos share that name.
- The local feature branch is kept.
- Removing the last repo tears down the session (`post-teardown` hooks run).
- A worktree that was deleted by hand is dropped from the session. A locked
  worktree record requires `--force`.

## Teardown

```sh
autofeat teardown FEATURE
```

- Stops without changes if any worktree is dirty. `--force` discards those
  changes. Use it only with explicit user authorization, and name the scope
  affected.
- Remote clones with unpushed commits trigger a warning. `--force` does not
  make those commits recoverable. Tell the user.
- Local feature branches stay in their source repos; remote clones are deleted
  entirely.

## Sync Conflicts And Rollback

A plain `sync` attempts to roll the session back on a rebase conflict or
failure. Read the rollback diagnostic before choosing the next step.

- `synchronization rolled back`: the pre-sync commits were restored. Report
  the conflicting files, then leave the session as is to retry later or rerun
  with `autofeat sync FEATURE --keep-conflicts` to resolve them now.
- `could not be fully rolled back`: inspect every reported repository error
  and run `autofeat status FEATURE` before retrying. Address any remaining
  rebase or failed restoration while preserving new work.

Rollback can fail while inspecting or aborting a rebase, or restoring an
already rebased repo with `git reset --keep`. For example, new local changes
can prevent restoration. A repo that gained commits or switched branches
after it was rebased is deliberately left unchanged. Do not assume a failed
rollback restored every repo.

After `--keep-conflicts` (or an interrupted sync), preserve the worktree and
choose one path:

| Goal | Action |
| --- | --- |
| Finish the rebase | Resolve, `git add`, `git -C WORKTREE rebase --continue` |
| Abort one repo | `git -C WORKTREE rebase --abort` |
| Abort all stuck repos | `autofeat sync SELECTOR... --cancel` |

- `--cancel` runs `git rebase --abort` only where a rebase is in progress. It
  never discards commits and reports each failure. A partial failure exits
  non-zero.
- `sync` refuses to start while a rebase is in progress; finish or cancel it
  first.

## Hook Failures

- `post-add` failure: later hooks are skipped and the new repo is removed.
  Report the error and retry after fixing the cause. Do not add partial state
  by hand.
- `post-teardown` failure: the session is already gone. Check with
  `autofeat list` before retrying.
