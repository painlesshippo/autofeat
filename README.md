# autofeat

`autofeat` manages disposable Git worktrees for concurrent AI-agent feature development. A feature session can group worktrees from multiple repositories and open them together in one VS Code workspace.

## Requirements

- Go 1.24 or later, to build or install the CLI.
- Git installed and available on `PATH`.
- VS Code's `code` command on `PATH` to use the default workspace-opening command. Another editor command can be configured instead.

On its first use, `autofeat` creates `$HOME/.autofeat/config.json`:

```json
{
  "workspace_base_dir": "/home/your-user/.autofeat-workspaces",
  "editor_cmd": "code"
}
```

Set `workspace_base_dir` to choose where feature worktrees and `.code-workspace` files are created. Set `editor_cmd` to the executable that should receive a workspace-file path.

## Installation

Install the latest released version:

```sh
go install github.com/painlesshippo/autofeat/cmd/autofeat@latest
```

Or build from a local clone:

```sh
git clone https://github.com/painlesshippo/autofeat.git
cd autofeat
go build -o autofeat ./cmd/autofeat
```

Ensure the installed binary directory, or the directory containing the built binary, is on `PATH`.

## Usage

Run `autofeat <feature-name>` inside a Git repository to add that repository to a feature session. The command creates a branch with the supplied feature name, adds a worktree, records the session, and regenerates its VS Code workspace. Feature names use valid Git branch syntax, so hierarchical names such as `feature/potato` and `bug/f321s-aaa` are supported.

```sh
cd ~/sources/repo1
autofeat feature/potato

cd ~/sources/repo2
autofeat feature/potato
```

Add a remote repository to a feature from any directory by passing its HTTP(S) or Git SSH URL. `autofeat` clones it into the feature directory and creates the supplied feature branch:

```sh
autofeat feature/potato https://github.com/example/repo3.git
```

Workspace directory names are flattened so they remain a single directory. `/` is escaped as `%2F` (and literal `%` as `%25`) to avoid collisions. With the default configuration, this produces:

```text
~/.autofeat-workspaces/
└── feature%2Fpotato/
    ├── repo1/
    ├── repo2/
    ├── repo3/
    └── feature%2Fpotato.code-workspace
```

Open a session explicitly from any directory:

```sh
autofeat feature/potato open
```

Running the feature command outside a Git repository also opens an existing session:

```sh
cd ~
autofeat feature/potato
```

List active sessions:

```sh
autofeat list
```

Print the build version, commit, and timestamp:

```sh
autofeat version
```

Tear down a session after confirming all worktrees are clean:

```sh
autofeat feature/potato teardown
```

If a worktree or cloned remote repository has uncommitted changes, teardown stops without removing anything. To intentionally discard those changes while removing the worktrees, use:

```sh
autofeat feature/potato teardown --force
```

Before deleting a cloned remote repository, `autofeat` warns when its feature branch has commits that have not been pushed to the corresponding remote branch.
