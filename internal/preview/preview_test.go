package preview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/painlesshippo/autofeat/internal/state"
)

func TestBuildSortsSessionsAndRetainsRepositoryErrors(t *testing.T) {
	report := Build(map[string]state.Session{
		"z-feature": {
			Repos: []state.Repository{{Name: "z-repository", WorktreePath: "/does/not/exist"}},
		},
		"a-feature": {
			Repos: []state.Repository{{Name: "a-repository", WorktreePath: "/also/does/not/exist"}},
		},
	}, "master", time.Date(2026, time.July, 22, 0, 0, 0, 0, time.UTC))

	if len(report.Sessions) != 2 {
		t.Fatalf("Build() sessions = %d, want 2", len(report.Sessions))
	}
	if report.Sessions[0].FeatureName != "a-feature" || report.Sessions[1].FeatureName != "z-feature" {
		t.Errorf("Build() session order = %#v, want alphabetical", report.Sessions)
	}
	if report.Sessions[0].Repositories[0].Error == "" {
		t.Error("Build() missing repository error, want retained Git failure")
	}
}

func TestRenderEscapesContentAndClassifiesDiffLines(t *testing.T) {
	contents, err := Render(Report{
		BaseRef:     "master<script>",
		GeneratedAt: time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC),
		Sessions: []Session{{
			FeatureName: "feature/<unsafe>",
			Repositories: []Repository{{
				Name: "repo<&>",
				Diff: "diff --git a/a.txt b/a.txt\n@@ -1 +1 @@\n-old <value>\n+new <value>\n",
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	html := string(contents)
	for _, want := range []string{"feature/&lt;unsafe&gt;", "repo&lt;&amp;&gt;", "master&lt;script&gt;", "diff-addition", "diff-deletion", "diff-hunk"} {
		if !strings.Contains(html, want) {
			t.Errorf("Render() output does not contain %q", want)
		}
	}
	if strings.Contains(html, "<unsafe>") || strings.Contains(html, "<value>") {
		t.Errorf("Render() output contains unescaped text: %s", html)
	}
}

func TestWriteSnapshotReplacesPrivateFile(t *testing.T) {
	snapshotPath := filepath.Join(t.TempDir(), ".autofeat", "preview.html")
	if err := WriteSnapshot(snapshotPath, []byte("first")); err != nil {
		t.Fatalf("WriteSnapshot() first write error = %v", err)
	}
	if err := WriteSnapshot(snapshotPath, []byte("second")); err != nil {
		t.Fatalf("WriteSnapshot() replacement error = %v", err)
	}

	contents, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "second" {
		t.Errorf("snapshot contents = %q, want replacement contents", contents)
	}
	info, err := os.Stat(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("snapshot permissions = %o, want 600", info.Mode().Perm())
	}
}
