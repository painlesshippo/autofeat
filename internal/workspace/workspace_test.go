package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewCreatesRelativeFolders(t *testing.T) {
	t.Parallel()

	got := New([]string{"repo1", "repo2"})
	if len(got.Folders) != 2 {
		t.Fatalf("folder count = %d, want 2", len(got.Folders))
	}
	if got.Folders[0].Path != "./repo1" || got.Folders[1].Path != "./repo2" {
		t.Errorf("folders = %+v, want ./repo1 and ./repo2", got.Folders)
	}
}

func TestWriteProducesWorkspaceJSON(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "feature1.code-workspace")
	if err := Write(path, []string{"repo1", "repo2"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	want := "{\n  \"folders\": [\n    {\n      \"path\": \"./repo1\"\n    },\n    {\n      \"path\": \"./repo2\"\n    }\n  ]\n}\n"
	if string(contents) != want {
		t.Errorf("workspace contents = %q, want %q", contents, want)
	}
}
