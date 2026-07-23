package preview

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	}, "master", "", time.Date(2026, time.July, 22, 0, 0, 0, 0, time.UTC))

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

func TestBuildPreservesOrderAfterConcurrentCollection(t *testing.T) {
	repositories := []state.Repository{
		{Name: "repository-z", WorktreePath: "z"},
		{Name: "repository-b", WorktreePath: "b"},
		{Name: "repository-a", WorktreePath: "a"},
		{Name: "repository-c", WorktreePath: "c"},
	}
	release := make(map[string]chan struct{}, len(repositories))
	for _, repository := range repositories {
		release[repository.WorktreePath] = make(chan struct{})
	}
	release["d"] = make(chan struct{})
	started := make(chan string, len(repositories))
	reportResult := make(chan Report, 1)

	go func() {
		reportResult <- buildWithDiff(map[string]state.Session{
			"feature/z": {Repos: repositories},
			"feature/a": {Repos: []state.Repository{{Name: "repository-d", WorktreePath: "d"}}},
		}, "master", "", time.Now(), func(destPath, _ string) (string, error) {
			started <- destPath
			<-release[destPath]
			return "diff " + destPath, nil
		})
	}()

	for range maxDiffWorkers {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("Build() did not begin concurrent diff collection")
		}
	}
	for _, gate := range release {
		close(gate)
	}

	var report Report
	select {
	case report = <-reportResult:
	case <-time.After(time.Second):
		t.Fatal("Build() did not finish after all diff jobs were released")
	}

	if got, want := report.Sessions[0].FeatureName, "feature/a"; got != want {
		t.Errorf("first feature = %q, want %q", got, want)
	}
	if got, want := report.Sessions[1].FeatureName, "feature/z"; got != want {
		t.Errorf("second feature = %q, want %q", got, want)
	}
	for index, want := range []string{"repository-a", "repository-b", "repository-c", "repository-z"} {
		if got := report.Sessions[1].Repositories[index].Name; got != want {
			t.Errorf("repository at index %d = %q, want %q", index, got, want)
		}
	}
}

func TestBuildBoundsConcurrentDiffCollection(t *testing.T) {
	repositories := make([]state.Repository, maxDiffWorkers+1)
	for index := range repositories {
		repositories[index] = state.Repository{
			Name:         "repository-" + string(rune('a'+index)),
			WorktreePath: "path-" + string(rune('a'+index)),
		}
	}

	var active atomic.Int32
	var maximum atomic.Int32
	started := make(chan struct{}, maxDiffWorkers)
	release := make(chan struct{})
	reportResult := make(chan Report, 1)
	go func() {
		reportResult <- buildWithDiff(map[string]state.Session{
			"feature": {Repos: repositories},
		}, "master", "", time.Now(), func(_, _ string) (string, error) {
			current := active.Add(1)
			setMaximum(&maximum, current)
			started <- struct{}{}
			<-release
			active.Add(-1)
			return "", nil
		})
	}()

	for range maxDiffWorkers {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("Build() did not start the expected number of workers")
		}
	}
	if got := maximum.Load(); got != maxDiffWorkers {
		t.Errorf("maximum concurrent diff calls = %d, want %d", got, maxDiffWorkers)
	}
	close(release)

	select {
	case <-reportResult:
	case <-time.After(time.Second):
		t.Fatal("Build() did not finish after releasing workers")
	}
}

func TestBuildRetainsMixedDiffResults(t *testing.T) {
	report := buildWithDiff(map[string]state.Session{
		"feature": {
			Repos: []state.Repository{
				{Name: "failure", WorktreePath: "failure"},
				{Name: "success", WorktreePath: "success"},
			},
		},
	}, "master", "", time.Now(), func(destPath, _ string) (string, error) {
		if destPath == "failure" {
			return "", errors.New("missing base reference")
		}
		return "diff success", nil
	})

	repositories := report.Sessions[0].Repositories
	if repositories[0].Error != "missing base reference" {
		t.Errorf("failure repository error = %q, want missing base reference", repositories[0].Error)
	}
	if repositories[1].Diff != "diff success" || repositories[1].Error != "" {
		t.Errorf("success repository = %+v, want successful diff", repositories[1])
	}
}

func TestBuildUsesRepositoryBaseBranchesAndOverrides(t *testing.T) {
	sessions := map[string]state.Session{
		"feature": {
			Repos: []state.Repository{
				{Name: "main-repository", WorktreePath: "main", BaseBranch: "main"},
				{Name: "default-repository", WorktreePath: "default"},
			},
		},
	}

	var basesByPath = make(map[string]string)
	report := buildWithDiff(sessions, "master", "", time.Now(), func(path, base string) (string, error) {
		basesByPath[path] = base
		return "", nil
	})
	if basesByPath["main"] != "main" || basesByPath["default"] != "master" {
		t.Errorf("stored bases = %#v, want main and master", basesByPath)
	}
	if report.Sessions[0].Repositories[0].BaseBranch != "master" || report.Sessions[0].Repositories[1].BaseBranch != "main" {
		t.Errorf("report repositories = %#v, want sorted repositories with their bases", report.Sessions[0].Repositories)
	}

	basesByPath = make(map[string]string)
	buildWithDiff(sessions, "master", "develop", time.Now(), func(path, base string) (string, error) {
		basesByPath[path] = base
		return "", nil
	})
	if basesByPath["main"] != "develop" || basesByPath["default"] != "develop" {
		t.Errorf("override bases = %#v, want develop for every repository", basesByPath)
	}
}

func setMaximum(maximum *atomic.Int32, current int32) {
	for {
		previous := maximum.Load()
		if current <= previous || maximum.CompareAndSwap(previous, current) {
			return
		}
	}
}

func TestDiffFilesGroupsLinesAndExtractsPaths(t *testing.T) {
	files := diffFiles("diff --git a/old.txt b/old.txt\n--- a/old.txt\n+++ /dev/null\n-deleted\ndiff --git a/new.txt b/new.txt\n--- /dev/null\n+++ b/new.txt\n+added\ndiff --git \"a/image file.png\" \"b/image file.png\"\nBinary files differ\n")

	if len(files) != 3 {
		t.Fatalf("diffFiles() returned %d files, want 3", len(files))
	}
	if files[0].Name != "old.txt" || files[1].Name != "new.txt" || files[2].Name != "image file.png" {
		t.Errorf("diffFiles() names = %q, %q, %q, want old.txt, new.txt, image file.png", files[0].Name, files[1].Name, files[2].Name)
	}
	if got := files[1].Lines[len(files[1].Lines)-1]; got.Class != "diff-addition" || got.Text != "+added\n" {
		t.Errorf("last new-file line = %+v, want classified addition with newline", got)
	}
}

func TestRenderEscapesContentAndClassifiesDiffLines(t *testing.T) {
	contents, err := Render(Report{
		BaseRef:     "master<script>",
		GeneratedAt: time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC),
		Sessions: []Session{
			{
				FeatureName: "feature/<unsafe>",
				Repositories: []Repository{{
					Name: "repo<&>",
					Diff: "diff --git a/a.txt b/a.txt\n@@ -1 +1 @@\n-old <value>\n+new <value>\n",
				}},
			},
			{FeatureName: "feature/two"},
		},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	html := string(contents)
	for _, want := range []string{"feature/&lt;unsafe&gt;", "repo&lt;&amp;&gt;", "a.txt", "master&lt;script&gt;", "file-change", "diff-addition", "diff-deletion", "diff-hunk", "background: #2da44e33"} {
		if !strings.Contains(html, want) {
			t.Errorf("Render() output does not contain %q", want)
		}
	}
	if strings.Contains(html, "<unsafe>") || strings.Contains(html, "<value>") {
		t.Errorf("Render() output contains unescaped text: %s", html)
	}
	for _, want := range []string{`id="feature-0" checked`, `for="feature-0"`, `id="feature-1"`, `for="feature-1"`, `.feature-tab-input:checked`, `type="radio"`} {
		if !strings.Contains(html, want) {
			t.Errorf("Render() tab output does not contain %q", want)
		}
	}
	if strings.Contains(html, "<script") {
		t.Errorf("Render() output contains JavaScript: %s", html)
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
