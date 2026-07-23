// Package review builds static HTML snapshots of autofeat worktree changes.
package review

import (
	"sort"
	"sync"
	"time"

	gitcmd "github.com/painlesshippo/autofeat/internal/git"
	"github.com/painlesshippo/autofeat/internal/state"
)

const maxDiffWorkers = 4

// Report contains the change data rendered into a review snapshot.
type Report struct {
	BaseRef     string
	GeneratedAt time.Time
	Sessions    []Session
}

// Session contains one feature session's repository reviews.
type Session struct {
	FeatureName   string
	CreatedAt     time.Time
	FeatureDir    string
	WorkspaceFile string
	Repositories  []Repository
}

// Repository contains the review result for one worktree.
type Repository struct {
	Name       string
	BaseBranch string
	BaseRef    string
	Ahead      int
	Behind     int
	Diff       string
	Error      string
}

type diffFunc func(destPath, baseRef string) (string, error)
type repositoryDataFunc func(destPath, baseBranch string) (string, gitcmd.BaseStatus, error)

type diffJob struct {
	sessionIndex    int
	repositoryIndex int
	repository      state.Repository
	baseBranch      string
}

// Build collects a worktree diff for every repository in sessions. A failure
// for one repository is retained in its report so other reviews remain useful.
func Build(sessions map[string]state.Session, defaultBaseBranch, overrideBaseBranch string, generatedAt time.Time) Report {
	return buildWithRepositoryData(sessions, defaultBaseBranch, overrideBaseBranch, generatedAt, func(destPath, baseBranch string) (string, gitcmd.BaseStatus, error) {
		status, err := gitcmd.CachedBaseStatus(destPath, baseBranch)
		if err != nil {
			return "", gitcmd.BaseStatus{}, err
		}
		diff, err := gitcmd.Diff(destPath, status.BaseRef)
		return diff, status, err
	})
}

func buildWithDiff(sessions map[string]state.Session, defaultBaseBranch, overrideBaseBranch string, generatedAt time.Time, collectDiff diffFunc) Report {
	return buildWithRepositoryData(sessions, defaultBaseBranch, overrideBaseBranch, generatedAt, func(destPath, baseBranch string) (string, gitcmd.BaseStatus, error) {
		diff, err := collectDiff(destPath, baseBranch)
		return diff, gitcmd.BaseStatus{}, err
	})
}

func buildWithRepositoryData(sessions map[string]state.Session, defaultBaseBranch, overrideBaseBranch string, generatedAt time.Time, collectData repositoryDataFunc) Report {
	featureNames := make([]string, 0, len(sessions))
	for featureName := range sessions {
		featureNames = append(featureNames, featureName)
	}
	sort.Strings(featureNames)

	report := Report{
		BaseRef:     reviewBaseDescription(defaultBaseBranch, overrideBaseBranch),
		GeneratedAt: generatedAt.UTC(),
		Sessions:    make([]Session, 0, len(featureNames)),
	}
	jobs := make([]diffJob, 0)
	for sessionIndex, featureName := range featureNames {
		session := sessions[featureName]
		repositories := append([]state.Repository(nil), session.Repos...)
		sort.Slice(repositories, func(i, j int) bool {
			if repositories[i].Name == repositories[j].Name {
				return repositories[i].WorktreePath < repositories[j].WorktreePath
			}
			return repositories[i].Name < repositories[j].Name
		})

		reviewSession := Session{
			FeatureName:   featureName,
			CreatedAt:     session.CreatedAt,
			FeatureDir:    session.FeatureDir,
			WorkspaceFile: session.WorkspaceFile,
			Repositories:  make([]Repository, len(repositories)),
		}
		for repositoryIndex, repository := range repositories {
			jobs = append(jobs, diffJob{
				sessionIndex:    sessionIndex,
				repositoryIndex: repositoryIndex,
				repository:      repository,
				baseBranch:      selectBaseBranch(repository.BaseBranch, defaultBaseBranch, overrideBaseBranch),
			})
		}

		report.Sessions = append(report.Sessions, reviewSession)
	}

	collectRepositoryData(&report, jobs, collectData)

	return report
}

func collectRepositoryData(report *Report, jobs []diffJob, collectData repositoryDataFunc) {
	workerCount := min(maxDiffWorkers, len(jobs))
	if workerCount == 0 {
		return
	}

	jobQueue := make(chan diffJob)
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for job := range jobQueue {
				diff, status, err := collectData(job.repository.WorktreePath, job.baseBranch)
				result := Repository{
					Name:       job.repository.Name,
					BaseBranch: job.baseBranch,
					BaseRef:    status.BaseRef,
					Ahead:      status.Ahead,
					Behind:     status.Behind,
					Diff:       diff,
				}
				if err != nil {
					result.Error = err.Error()
				}
				report.Sessions[job.sessionIndex].Repositories[job.repositoryIndex] = result
			}
		}()
	}

	for _, job := range jobs {
		jobQueue <- job
	}
	close(jobQueue)
	workers.Wait()
}

func selectBaseBranch(repositoryBaseBranch, defaultBaseBranch, overrideBaseBranch string) string {
	if overrideBaseBranch != "" {
		return overrideBaseBranch
	}
	if repositoryBaseBranch != "" {
		return repositoryBaseBranch
	}
	return defaultBaseBranch
}

func reviewBaseDescription(defaultBaseBranch, overrideBaseBranch string) string {
	if overrideBaseBranch != "" {
		return overrideBaseBranch
	}
	return "repository defaults (" + defaultBaseBranch + ")"
}
