# Task: Build `autofeat` - Ephemeral AI Agent Worktree CLI

## Objective
Build a Go-based CLI tool named `autofeat` that manages ephemeral Git worktrees for concurrent AI agent development. The tool uses an additive workflow: developers run the tool inside a Git repository to dynamically attach it to an active "feature session". Worktrees from multiple repositories are grouped into a standardized, configurable destination folder and bound together by a generated VS Code workspace.

## Tech Stack & Constraints
- **Language**: Go (1.24+)
- **Minimize Dependencies**: Do NOT use heavy CLI frameworks like Cobra or Viper. Use standard library `flag` and `os.Args` for CLI routing, `text/tabwriter` for tables, and `encoding/json` for configurations.
- **State & Config Management**: Use standard **JSON** exclusively for global configuration and state tracking.
- **Git Interaction**: Use standard `os/exec` wrapping the system `git` binary (Do NOT use `go-git`).

---

## Step 1: Project Scaffolding
1. Initialize the Go module (`go mod init github.com/painlesshippo/autofeat`).
2. Create the idiomatic Go directory structure:
   - `cmd/autofeat/` (Main entry point & dynamic CLI routing)
   - `internal/git/` (os/exec wrappers for Git repo detection and worktrees)
   - `internal/config/` (Global JSON config management)
   - `internal/state/` (Global JSON state management)
   - `internal/workspace/` (VS Code workspace generation)

---

## Step 2: Global Config & Resulting Folder Structure

**Global Configuration (`~/.autofeat/config.json`)**
The tool must read/write a global config to define where sessions live and how to open them.
```json
{
  "workspace_base_dir": "/Users/name/.autofeat-workspaces",
  "editor_cmd": "code"
}
```

**Folder Structure Example**
If a developer runs `autofeat feature1` inside `~/sources/repo1`, and then again inside `~/sources/repo2`, the resulting filesystem must look like this:

```text
~/sources/repo1/ (Main repo)
~/sources/repo2/ (Main repo)

~/.autofeat-workspaces/
└── feature1/
    ├── repo1/ (Git worktree linked to ~/sources/repo1)
    ├── repo2/ (Git worktree linked to ~/sources/repo2)
    └── feature1.code-workspace (Generated workspace file)
```

---

## Step 3: Data Models & Setup

**A. State Management (`internal/state`)**
- Use a global state file at `~/.autofeat/state.json`.
- The JSON structure must look exactly like this:
  ```json
  {
    "sessions": {
      "feature1": {
        "created_at": "2026-07-21T10:00:00Z",
        "feature_dir": "/Users/name/.autofeat-workspaces/feature1",
        "workspace_file": "/Users/name/.autofeat-workspaces/feature1/feature1.code-workspace",
        "repos": [
          {
            "name": "repo1",
            "original_path": "/Users/name/sources/repo1",
            "worktree_path": "/Users/name/.autofeat-workspaces/feature1/repo1"
          },
          {
            "name": "repo2",
            "original_path": "/Users/name/sources/repo2",
            "worktree_path": "/Users/name/.autofeat-workspaces/feature1/repo2"
          }
        ]
      }
    }
  }
  ```
- Write functions: `SaveSession()`, `GetSession()`, `ListSessions()`, `UpdateSession()`, and `DeleteSession()`. 

**B. VS Code Workspace (`internal/workspace`)**
- Define strict Go structs for `.code-workspace` JSON generation.
- Struct must map to: `{"folders": [{"path": "./repo1"}, {"path": "./repo2"}]}`. *(Use relative paths since the workspace file sits adjacent to the worktrees).*

**C. Git Package (`internal/git`)**
Implement Go wrappers using `os/exec`:
- `IsInsideWorkTree()` -> Runs `git rev-parse --is-inside-work-tree`. Returns true if inside a git repo.
- `GetRepoRoot()` -> Runs `git rev-parse --show-toplevel` to get the absolute path of the current main repo.
- `AddWorktree(branchName, destPath)` -> `git worktree add <destPath> -b <branchName>`
- `RemoveWorktree(destPath, force)` -> `git worktree remove <destPath>`
- `HasUncommittedChanges(destPath)` -> Parse `git status --porcelain`. Return true if output length > 0.

---

## Step 4: Implementing CLI Routing (`cmd/autofeat/main.go`)

Because the commands are dynamic, parse `os.Args` directly (rather than relying strictly on the `flag` package).

**1. `autofeat list`**
- Queries `state.json` and uses standard `text/tabwriter` to print a table of active feature sessions.

**2. `autofeat <feature-name>` (The additive/implicit workflow)**
- Read global config to get `workspace_base_dir`.
- Check if the current working directory is a git repo (`git.IsInsideWorkTree()`).
- **IF YES (Add Repo):**
  1. Get the absolute path of the current repo (`git.GetRepoRoot()`). Extract the repo's folder name.
  2. Create the target worktree path: `<workspace_base_dir>/<feature-name>/<repo-name>`.
  3. Ensure the base feature directory exists.
  4. Run `git.AddWorktree("agent/<feature-name>", <target-path>)`.
  5. Load `<feature-name>` from `state.json` (or initialize a new session object if it's the first repo).
  6. Append the new repo to the session's `repos` array. Save to `state.json`.
  7. Regenerate the `../<feature-name>.code-workspace` file to include all attached repos.
  8. Print: *"Added <repo-name> to feature <feature-name>"*.
- **IF NO (Implicit Open):**
  1. Load `<feature-name>` from `state.json`.
  2. If session doesn't exist, throw error.
  3. Read `editor_cmd` from global config (default: `"code"`).
  4. Run `exec.Command(<editor_cmd>, <workspace_file_path>)` to open the session.

**3. `autofeat <feature-name> open`**
- Explicitly performs the "Implicit Open" logic above, regardless of whether the user is currently in a git repo.

**4. `autofeat <feature-name> teardown`**
- Look up the session in `state.json`.
- For each `worktree_path` in the session's repos array:
  - Run `git.HasUncommittedChanges()`. Warn user and abort if dirty (unless handled via an interactive prompt or force flag).
  - Run `git.RemoveWorktree()`.
- Delete the entire `<workspace_base_dir>/<feature-name>` directory (which removes the workspace file and cleans up empty folders).
- Remove the session entry from `state.json`.

---

## Execution Rules for the AI Agent
1. Work step-by-step. Start with the `internal/` packages before wiring up the `cmd/autofeat/main.go` entry point.
2. Minimize dependencies: Do NOT use Cobra, Viper, or external UI libraries. Stick to the Go standard library (`os`, `os/exec`, `encoding/json`, `path/filepath`, `text/tabwriter`) exclusively.
3. Do not use external libraries for Git. Use Go standard library `os/exec` exclusively.
4. Ensure cross-platform path handling by using Go's `path/filepath` package when joining directories.