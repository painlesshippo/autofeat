// Package preview builds static HTML snapshots of autofeat worktree changes.
package preview

import (
	"sort"
	"time"

	gitcmd "github.com/painlesshippo/autofeat/internal/git"
	"github.com/painlesshippo/autofeat/internal/state"
)

// Report contains the change data rendered into a preview snapshot.
type Report struct {
	BaseRef     string
	GeneratedAt time.Time
	Sessions    []Session
}

// Session contains one feature session's repository previews.
type Session struct {
	FeatureName  string
	Repositories []Repository
}

// Repository contains the preview result for one worktree.
type Repository struct {
	Name  string
	Diff  string
	Error string
}

// Build collects a worktree diff for every repository in sessions. A failure
// for one repository is retained in its report so other previews remain useful.
func Build(sessions map[string]state.Session, baseRef string, generatedAt time.Time) Report {
	featureNames := make([]string, 0, len(sessions))
	for featureName := range sessions {
		featureNames = append(featureNames, featureName)
	}
	sort.Strings(featureNames)

	report := Report{
		BaseRef:     baseRef,
		GeneratedAt: generatedAt.UTC(),
		Sessions:    make([]Session, 0, len(featureNames)),
	}
	for _, featureName := range featureNames {
		session := sessions[featureName]
		repositories := append([]state.Repository(nil), session.Repos...)
		sort.Slice(repositories, func(i, j int) bool {
			if repositories[i].Name == repositories[j].Name {
				return repositories[i].WorktreePath < repositories[j].WorktreePath
			}
			return repositories[i].Name < repositories[j].Name
		})

		previewSession := Session{
			FeatureName:  featureName,
			Repositories: make([]Repository, 0, len(repositories)),
		}
		for _, repository := range repositories {
			diff, err := gitcmd.Diff(repository.WorktreePath, baseRef)
			previewRepository := Repository{Name: repository.Name, Diff: diff}
			if err != nil {
				previewRepository.Error = err.Error()
			}
			previewSession.Repositories = append(previewSession.Repositories, previewRepository)
		}

		report.Sessions = append(report.Sessions, previewSession)
	}

	return report
}
