# Safety And Recovery

Always run `autofeat status FEATURE` before a destructive operation.

## Remove One Repository

```sh
autofeat remove FEATURE repo1                 # worktree directory name
autofeat remove FEATURE --local /path/inside/repo
autofeat remove FEATURE --remote URL
```

- `REPO` matches the worktree directory first, then the repository name. It
  fails if several repos share that name.
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

## Rebase Recovery

If `sync` stops on a conflict, preserve the worktree and choose one path:

| Goal | Action |
| --- | --- |
| Finish the rebase | Resolve, `git add`, `git -C WORKTREE rebase --continue` |
| Abort one repo | `git -C WORKTREE rebase --abort` |
| Abort all stuck repos | `autofeat sync SELECTOR... --cancel` |

- `--cancel` runs `git rebase --abort` only where a rebase is in progress. It
  never discards commits and reports each failure. A partial failure exits
  non-zero.
- Do not start another `sync` while a rebase is still in progress.

## Hook Failures

- `post-add` failure: later hooks are skipped and the new repo is removed.
  Report the error and retry after fixing the cause. Do not add partial state
  by hand.
- `post-teardown` failure: the session is already gone. Check with
  `autofeat list` before retrying.
