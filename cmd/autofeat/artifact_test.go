package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/painlesshippo/autofeat/internal/artifact"
	"github.com/painlesshippo/autofeat/internal/state"
)

func saveArtifactSession(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if err := state.SaveSession("feature/art", state.Session{Repos: []state.Repository{
		{Name: "svc", WorktreePath: "/tmp/feature-art/svc-1"},
		{Name: "svc", WorktreePath: "/tmp/feature-art/svc-2"},
		{Name: "web", WorktreePath: "/tmp/feature-art/web"},
	}}); err != nil {
		t.Fatal(err)
	}
}

func TestArtifactCommandsAddListRemove(t *testing.T) {
	saveArtifactSession(t)
	const prURL = "https://dev.azure.com/org/Proj/_git/web/pullrequest/12"

	var output bytes.Buffer
	if err := addArtifactTo(&output, "feature/art", prURL, "", "web"); err != nil {
		t.Fatalf("addArtifactTo() error = %v", err)
	}
	if !strings.Contains(output.String(), "Recorded ado-pull-request 12 on feature feature/art") {
		t.Errorf("add output = %q", output.String())
	}

	output.Reset()
	if err := addArtifactTo(&output, "feature/art", prURL+"/", "", ""); err != nil {
		t.Fatalf("addArtifactTo(duplicate) error = %v", err)
	}
	if !strings.Contains(output.String(), "already recorded") {
		t.Errorf("duplicate add output = %q, want already recorded", output.String())
	}

	if err := addArtifactTo(&output, "feature/art", "555", "ado-build", "svc-2"); err != nil {
		t.Fatalf("addArtifactTo(--type) error = %v", err)
	}
	if err := addArtifactTo(&output, "feature/art", "555", "", ""); err == nil || !strings.Contains(err.Error(), "--type") {
		t.Errorf("addArtifactTo(plain number) error = %v, want --type hint", err)
	}
	if err := addArtifactTo(&output, "feature/art", "!3", "", "svc"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("addArtifactTo(ambiguous repo) error = %v, want ambiguous", err)
	}
	if err := addArtifactTo(&output, "feature/art", "!3", "", "missing"); err == nil {
		t.Error("addArtifactTo(unknown repo) error = nil")
	}
	if err := addArtifactTo(&output, "feature/none", "!3", "", ""); err == nil {
		t.Error("addArtifactTo(unknown feature) error = nil")
	}

	output.Reset()
	if err := listArtifactsTo(&output, "feature/art", true); err != nil {
		t.Fatalf("listArtifactsTo(json) error = %v", err)
	}
	var listed []artifact.Artifact
	if err := json.Unmarshal(output.Bytes(), &listed); err != nil {
		t.Fatalf("list JSON: %v\n%s", err, output.String())
	}
	if len(listed) != 2 || listed[0].Repo != "web" || listed[1].Type != artifact.ADOBuild || listed[1].ID != "555" || listed[1].Repo != "svc-2" || listed[0].CreatedAt.IsZero() {
		t.Fatalf("listed artifacts = %+v", listed)
	}

	output.Reset()
	if err := listArtifactsTo(&output, "feature/art", false); err != nil {
		t.Fatalf("listArtifactsTo(table) error = %v", err)
	}
	for _, want := range []string{"TYPE", "ID", "REPO", "CREATED", "VALUE", "ado-pull-request", prURL, "svc-2"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("artifact table does not contain %q:\n%s", want, output.String())
		}
	}

	if err := removeArtifactTo(&output, "feature/art", "555", "", ""); err != nil {
		t.Fatalf("removeArtifactTo(id) error = %v", err)
	}
	if err := removeArtifactTo(&output, "feature/art", prURL+"/", "", ""); err != nil {
		t.Fatalf("removeArtifactTo(equivalent url) error = %v", err)
	}
	if err := removeArtifactTo(&output, "feature/art", prURL, "", ""); err == nil {
		t.Error("removeArtifactTo(already removed) error = nil")
	}

	output.Reset()
	if err := listArtifactsTo(&output, "feature/art", true); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != "[]" {
		t.Errorf("empty artifact JSON = %q, want []", output.String())
	}
}

func TestFindArtifactRejectsAmbiguousID(t *testing.T) {
	artifacts := []artifact.Artifact{
		{Type: artifact.ADOBuild, Value: "https://dev.azure.com/o/p/_build/results?buildId=9", ID: "9"},
		{Type: artifact.GitHubIssue, Value: "https://github.com/o/r/issues/9", ID: "9"},
	}
	if _, err := findArtifact("feature", artifacts, "9", "", ""); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("findArtifact(9) error = %v, want ambiguous", err)
	}
	if index, err := findArtifact("feature", artifacts, artifacts[1].Value, "", ""); err != nil || index != 1 {
		t.Errorf("findArtifact(value) = (%d, %v), want (1, nil)", index, err)
	}
	if index, err := findArtifact("feature", artifacts, "9", artifact.GitHubIssue, ""); err != nil || index != 1 {
		t.Errorf("findArtifact(9, --type github-issue) = (%d, %v), want (1, nil)", index, err)
	}
}

func TestArtifactPlainIDsAreScopedByTypeAndRepository(t *testing.T) {
	saveArtifactSession(t)

	var output bytes.Buffer
	for _, add := range []struct {
		reference    string
		artifactType artifact.Type
		repository   string
	}{
		{"9", artifact.ADOBuild, ""},
		{"9", artifact.ADOWorkItem, ""},
		{"12", artifact.GitHubPullRequest, "web"},
		{"12", artifact.GitHubPullRequest, "svc-1"},
	} {
		if err := addArtifactTo(&output, "feature/art", add.reference, add.artifactType, add.repository); err != nil {
			t.Fatalf("addArtifactTo(%+v) error = %v", add, err)
		}
	}
	session, err := state.GetSession("feature/art")
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Artifacts) != 4 {
		t.Fatalf("artifacts = %+v, want four distinct records", session.Artifacts)
	}

	if err := removeArtifactTo(&output, "feature/art", "9", "", ""); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("removeArtifactTo(9) error = %v, want ambiguous", err)
	}
	if err := removeArtifactTo(&output, "feature/art", "12", "", ""); err == nil || !strings.Contains(err.Error(), "--repo") {
		t.Errorf("removeArtifactTo(12) error = %v, want ambiguity hint", err)
	}
	if err := removeArtifactTo(&output, "feature/art", "9", "bogus", ""); err == nil || !strings.Contains(err.Error(), "unknown artifact type") {
		t.Errorf("removeArtifactTo(--type bogus) error = %v", err)
	}
	if err := removeArtifactTo(&output, "feature/art", "9", artifact.ADOWorkItem, ""); err != nil {
		t.Fatalf("removeArtifactTo(9 --type ado-work-item) error = %v", err)
	}
	if err := removeArtifactTo(&output, "feature/art", "12", "", "web"); err != nil {
		t.Fatalf("removeArtifactTo(12 --repo web) error = %v", err)
	}

	session, err = state.GetSession("feature/art")
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Artifacts) != 2 || session.Artifacts[0].Type != artifact.ADOBuild || session.Artifacts[1].Repo != "svc-1" {
		t.Errorf("remaining artifacts = %+v, want ado-build 9 and the svc-1 pull request", session.Artifacts)
	}
}

func TestArtifactCommandDispatch(t *testing.T) {
	saveArtifactSession(t)

	if err := run([]string{"artifact"}); err == nil {
		t.Error("run(artifact) without subcommand error = nil")
	}
	if err := run([]string{"artifact", "add", "feature/art", "https://example.com/doc"}); err != nil {
		t.Fatalf("run(artifact add) error = %v", err)
	}
	if err := run([]string{"artifact", "add", "feature/art", "x", "--type", "bogus"}); err == nil {
		t.Error("run(artifact add --type bogus) error = nil")
	}
	if err := run([]string{"artifact", "list", "feature/art"}); err != nil {
		t.Fatalf("run(artifact list feature/art) error = %v", err)
	}
	if err := run([]string{"artifact", "list"}); err == nil {
		t.Error("run(artifact list) without feature error = nil")
	}
	if err := run([]string{"artifact", "remove", "feature/art", "https://example.com/doc"}); err != nil {
		t.Fatalf("run(artifact remove) error = %v", err)
	}
	if err := run([]string{"artifact", "add", "feature/art", "https://example.com/doc"}); err != nil {
		t.Fatalf("run(artifact add) error = %v", err)
	}
	session, err := state.GetSession("feature/art")
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Artifacts) != 1 || session.Artifacts[0].Type != artifact.Link {
		t.Errorf("artifacts = %+v, want one link", session.Artifacts)
	}

	values, _ := completeFeatureThenNothing(nil, nil, "feature/")
	if len(values) != 1 || values[0] != "feature/art" {
		t.Errorf("completeFeatureThenNothing(0 args) = %v, want [feature/art]", values)
	}
	if values, _ := completeFeatureThenNothing(nil, []string{"feature/art"}, ""); values != nil {
		t.Errorf("completeFeatureThenNothing(1 arg) = %v, want nil", values)
	}
	values, _ = completeArtifactTypes(nil, nil, "github-p")
	if len(values) != 1 || values[0] != "github-pull-request" {
		t.Errorf("completeArtifactTypes(github-p) = %v", values)
	}
	values, _ = completeArtifactRepositories(nil, []string{"feature/art"}, "svc")
	if strings.Join(values, ",") != "svc-1,svc-2" {
		t.Errorf("completeArtifactRepositories(svc) = %v", values)
	}
	if values, _ := completeArtifactRepositories(nil, nil, ""); values != nil {
		t.Errorf("completeArtifactRepositories(0 args) = %v, want nil", values)
	}
	if values, _ := completeArtifactRepositories(nil, []string{"feature/missing"}, ""); values != nil {
		t.Errorf("completeArtifactRepositories(missing) = %v, want nil", values)
	}
	values, _ = completeArtifactReferences(nil, nil, "feature/")
	if len(values) != 1 || values[0] != "feature/art" {
		t.Errorf("completeArtifactReferences(0 args) = %v, want [feature/art]", values)
	}
	values, _ = completeArtifactReferences(nil, []string{"feature/art"}, "https://ex")
	if len(values) != 1 || values[0] != "https://example.com/doc" {
		t.Errorf("completeArtifactReferences() = %v", values)
	}
	if values, _ := completeArtifactReferences(nil, []string{"feature/art", "extra"}, ""); values != nil {
		t.Errorf("completeArtifactReferences(2 args) = %v, want nil", values)
	}
	if values, _ := completeArtifactReferences(nil, []string{"feature/missing"}, ""); values != nil {
		t.Errorf("completeArtifactReferences(missing) = %v, want nil", values)
	}
}

func TestStatusAndListShowArtifacts(t *testing.T) {
	saveArtifactSession(t)
	if err := addArtifactTo(&bytes.Buffer{}, "feature/art", "AB#77", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveSession("feature/plain", state.Session{}); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := statusSessionsTo(&output, []string{"*"}); err != nil {
		t.Fatalf("statusSessionsTo() error = %v", err)
	}
	report := output.String()
	if !strings.Contains(report, "ado-work-item") || !strings.Contains(report, "AB#77") || strings.Count(report, "TYPE") != 1 {
		t.Errorf("status report lacks a single artifact section:\n%s", report)
	}

	output.Reset()
	if err := statusSessionsTo(&output, []string{"feature/plain"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "TYPE") {
		t.Errorf("status without artifacts prints an artifact table:\n%s", output.String())
	}

	output.Reset()
	if err := listSessionsTo(&output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "ARTIFACTS") {
		t.Errorf("list report lacks ARTIFACTS column:\n%s", output.String())
	}
}

func TestDropRepositoryArtifactLinks(t *testing.T) {
	artifacts := []artifact.Artifact{{Repo: "api"}, {Repo: "web"}, {}}
	dropRepositoryArtifactLinks(artifacts, "api")
	if artifacts[0].Repo != "" || artifacts[1].Repo != "web" {
		t.Errorf("artifacts = %+v, want only api link cleared", artifacts)
	}
}

func TestPrintTeardownArtifacts(t *testing.T) {
	var output bytes.Buffer
	if err := printTeardownArtifacts(&output, "feature/x", nil); err != nil || output.Len() != 0 {
		t.Errorf("printTeardownArtifacts(nil) = (%q, %v), want no output", output.String(), err)
	}
	artifacts := []artifact.Artifact{{Type: artifact.GitHubRun, Value: "https://github.com/o/r/actions/runs/5", ID: "5", CreatedAt: time.Now()}}
	if err := printTeardownArtifacts(&output, "feature/x", artifacts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Artifacts recorded on feature feature/x") || !strings.Contains(output.String(), artifacts[0].Value) {
		t.Errorf("teardown artifacts output = %q", output.String())
	}
}

func TestDashboardDetailShowsArtifacts(t *testing.T) {
	sessions := []dashboardSession{{name: "feature/art", drift: "current", lastActivity: time.Now(), session: state.Session{Artifacts: []artifact.Artifact{
		{Type: artifact.ADOBuild, Value: "https://dev.azure.com/o/p/_build/results?buildId=4", ID: "4", Repo: "api"},
	}}}}
	model := newDashboardModel("autofeat", defaultStaleAfter)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 200, Height: 24})
	updated, _ = updated.Update(dashboardLoadedMsg{sessions: sessions})
	rendered := updated.(dashboardModel).render()
	if !strings.Contains(rendered, "artifacts") || !strings.Contains(rendered, "ado-build 4 · api") {
		t.Errorf("dashboard detail lacks artifacts:\n%s", rendered)
	}
}
