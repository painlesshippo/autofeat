package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefinitionValidate(t *testing.T) {
	t.Parallel()

	valid := Definition{When: PostAdd, IfFiles: []string{"mise.toml", ".mise.toml"}, Run: "mise install"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	tests := map[string]Definition{
		"unsupported event": {When: "post-sync", Run: "true"},
		"blank command":     {When: PostAdd, Run: " "},
		"blank file":        {When: PostAdd, IfFiles: []string{""}, Run: "true"},
		"absolute file":     {When: PostAdd, IfFiles: []string{"/tmp/mise.toml"}, Run: "true"},
		"parent traversal":  {When: PostAdd, IfFiles: []string{"../mise.toml"}, Run: "true"},
	}
	for name, definition := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := definition.Validate(); err == nil {
				t.Errorf("Validate(%+v) error = nil, want error", definition)
			}
		})
	}
}

func TestRunMatchesAnyFileAndUsesWorktreeDirectory(t *testing.T) {
	for _, fileName := range []string{"mise.toml", ".mise.toml"} {
		t.Run(fileName, func(t *testing.T) {
			worktreePath := t.TempDir()
			if err := os.WriteFile(filepath.Join(worktreePath, fileName), []byte("[tools]\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			definitions := []Definition{{
				When:    PostAdd,
				IfFiles: []string{"mise.toml", ".mise.toml"},
				Run:     "pwd > hook.log; printf first >> hook.log",
			}, {
				When: PostAdd,
				Run:  "printf second >> hook.log",
			}}
			if err := Run(definitions, PostAdd, worktreePath); err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			contents, err := os.ReadFile(filepath.Join(worktreePath, "hook.log"))
			if err != nil {
				t.Fatal(err)
			}
			want := worktreePath + "\nfirstsecond"
			if string(contents) != want {
				t.Errorf("hook.log = %q, want %q", contents, want)
			}
		})
	}
}

func TestRunSkipsNonmatchingHooks(t *testing.T) {
	worktreePath := t.TempDir()
	definitions := []Definition{{When: PostAdd, IfFiles: []string{"mise.toml"}, Run: "touch hook-ran"}}

	if err := Run(definitions, PostAdd, worktreePath); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(worktreePath, "hook-ran")); !os.IsNotExist(err) {
		t.Errorf("nonmatching hook output Stat() error = %v, want file not to exist", err)
	}
}

func TestRunStopsAfterFailure(t *testing.T) {
	worktreePath := t.TempDir()
	definitions := []Definition{{When: PostAdd, Run: "exit 23"}, {When: PostAdd, Run: "touch hook-ran"}}

	err := Run(definitions, PostAdd, worktreePath)
	if err == nil || !strings.Contains(err.Error(), "post-add") {
		t.Fatalf("Run() error = %v, want post-add hook error", err)
	}
	if _, err := os.Stat(filepath.Join(worktreePath, "hook-ran")); !os.IsNotExist(err) {
		t.Errorf("hook after failure Stat() error = %v, want file not to exist", err)
	}
}
