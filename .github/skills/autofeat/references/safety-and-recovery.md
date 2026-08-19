# Safety And Recovery

## Destructive Operations

Inspect the exact target before detaching or deleting resources:

```sh
autofeat status feature/name
```

Remove one repository by specifying the same kind of source used to add it:

```sh
autofeat remove feature/name --local /path/to/repository
autofeat remove feature/name --remote https://github.com/example/repository.git
```

A local path may point inside the original repository. Removing the final
repository tears down the entire session. Local feature branches are retained.

Delete every worktree and remote clone in a session with:

```sh
autofeat teardown feature/name
```

Without `--force`, removal and teardown stop on uncommitted changes. Do not
retry with `--force` unless the user explicitly authorizes discarding those
changes. Before acting on authorization, identify the affected feature and
repository scope.

Remote clones can contain commits that do not exist on their corresponding
remote branch. Autofeat warns before deleting such a clone. Surface that
warning to the user; `--force` does not make an unpushed commit recoverable.

## Rebase Recovery

If `sync` encounters a conflict, it stops at that repository and leaves the
rebase in progress. Preserve the worktree and follow the paths printed by
autofeat. The two valid recovery directions are:

```sh
git -C /path/to/worktree rebase --continue
git -C /path/to/worktree rebase --abort
```

Do not start another `autofeat sync` while the rebase remains in progress.
Resolve conflicts and continue only when the user wants the rebase completed;
abort when the user wants the feature branch restored to its pre-sync state.

## Hook Failure

`post-add` hooks run in each new worktree or remote clone. If a hook fails,
autofeat stops subsequent hooks and removes the newly added repository so the
operation can be retried. Report the hook's error rather than manually adding
partial session state.

`post-teardown` hooks run after session resources and state are removed. A hook
error at that point does not imply that the session still exists; inspect with
`autofeat list` before retrying cleanup.

## State Files

Avoid manually rewriting files under `$HOME/.autofeat` during ordinary
recovery. They are versioned data files, reject unknown fields, and may be
upgraded when autofeat writes them. Prefer CLI operations and inspect current
state before considering manual repair.
