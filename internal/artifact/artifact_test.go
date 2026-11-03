package artifact

import (
	"errors"
	"strings"
	"testing"
)

func TestInfer(t *testing.T) {
	t.Parallel()

	const armRG = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-app"
	const armStorage = armRG + "/providers/Microsoft.Storage/storageAccounts/stapp"
	tests := []struct {
		name     string
		value    string
		wantType Type
		wantID   string
	}{
		{"ado build dev.azure.com", "https://dev.azure.com/org/Proj/_build/results?buildId=123&view=results", ADOBuild, "123"},
		{"ado build visualstudio", "https://org.visualstudio.com/Proj/_build/results?buildId=7", ADOBuild, "7"},
		{"ado work item", "https://dev.azure.com/org/Proj/_workitems/edit/4567/", ADOWorkItem, "4567"},
		{"ado work item shorthand", "AB#4567", ADOWorkItem, "4567"},
		{"ado work item shorthand lowercase", "ab#12", ADOWorkItem, "12"},
		{"ado pull request", "https://dev.azure.com/org/Proj/_git/my%20repo/pullrequest/89", ADOPullRequest, "89"},
		{"ado pull request visualstudio", "https://org.visualstudio.com/Proj/_git/repo/pullrequest/3?_a=overview", ADOPullRequest, "3"},
		{"ado pull request shorthand", "!45", ADOPullRequest, "45"},
		{"ado commit", "https://dev.azure.com/org/Proj/_git/repo/commit/abc123def", ADOCommit, "abc123def"},
		{"ado branch", "https://dev.azure.com/org/Proj/_git/repo?version=GBfeature%2Fx", ADOBranch, "feature/x"},
		{"ado repo", "https://dev.azure.com/org/Proj/_git/my%20repo", ADORepo, "my repo"},
		{"ado unknown page", "https://dev.azure.com/org/Proj/_wiki/wikis/home", Link, ""},
		{"ado build without id", "https://dev.azure.com/org/Proj/_build?definitionId=4", Link, ""},
		{"arm resource group", armRG, AzureResource, armRG},
		{"arm resource", armStorage, AzureResource, armStorage},
		{"arm child resource", armStorage + "/blobServices/default", AzureResource, armStorage + "/blobServices/default"},
		{"portal resource with blade", "https://portal.azure.com/#@contoso.onmicrosoft.com/resource" + armStorage + "/overview", AzureResource, armStorage},
		{"portal resource group with blade", "https://ms.portal.azure.com/#@tenant/resource" + armRG + "/overview", AzureResource, armRG},
		{"portal without resource", "https://portal.azure.com/#home", Link, ""},
		{"github pull request", "https://github.com/o/r/pull/12/files", GitHubPullRequest, "12"},
		{"github issue", "https://github.com/o/r/issues/34", GitHubIssue, "34"},
		{"github commit", "https://github.com/o/r/commit/deadbeef", GitHubCommit, "deadbeef"},
		{"github run", "https://github.com/o/r/actions/runs/987/job/1", GitHubRun, "987"},
		{"github repo", "https://github.com/o/r", Link, ""},
		{"github unknown section", "https://github.com/o/r/releases/tag/v1", Link, ""},
		{"ado git without repo", "https://dev.azure.com/org/Proj/_git", Link, ""},
		{"portal incomplete subscription path", "https://portal.azure.com/#@tenant/resource/subscriptions/only-sub", Link, ""},
		{"generic link", "  https://example.com/x  ", Link, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			gotType, gotID, err := Infer(test.value)
			if err != nil {
				t.Fatalf("Infer(%q) error = %v", test.value, err)
			}
			if gotType != test.wantType || gotID != test.wantID {
				t.Errorf("Infer(%q) = (%q, %q), want (%q, %q)", test.value, gotType, gotID, test.wantType, test.wantID)
			}
		})
	}
}

func TestInferRejectsUnrecognizedValues(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"123", "AB#", "!x", "some text", "ftp://example.com/x", "/subscriptions/x"} {
		if _, _, err := Infer(value); !errors.Is(err, ErrUnrecognized) {
			t.Errorf("Infer(%q) error = %v, want ErrUnrecognized", value, err)
		}
	}
}

func TestParseWithForcedType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		forced  Type
		want    Artifact
		wantErr string
	}{
		{name: "plain number", value: "123", forced: ADOBuild, want: Artifact{Type: ADOBuild, Value: "123", ID: "123"}},
		{name: "matching inferred type keeps id", value: "AB#9", forced: ADOWorkItem, want: Artifact{Type: ADOWorkItem, Value: "AB#9", ID: "9"}},
		{name: "link override", value: "https://github.com/o/r/pull/1", forced: Link, want: Artifact{Type: Link, Value: "https://github.com/o/r/pull/1"}},
		{name: "on-prem url", value: "https://tfs.corp/c/p/_build/results?buildId=5", forced: ADOBuild, want: Artifact{Type: ADOBuild, Value: "https://tfs.corp/c/p/_build/results?buildId=5", ID: "https://tfs.corp/c/p/_build/results?buildId=5"}},
		{name: "contradiction", value: "https://github.com/o/r/pull/1", forced: ADOBuild, wantErr: "looks like github-pull-request"},
		{name: "unknown type", value: "x", forced: "nope", wantErr: "unknown artifact type"},
		{name: "empty", value: " ", wantErr: "required"},
		{name: "plain number needs type", value: "123", wantErr: "pass --type"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := Parse(test.value, test.forced)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("Parse() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if got != test.want {
				t.Errorf("Parse() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestKeyNormalizesEquivalentReferences(t *testing.T) {
	t.Parallel()

	arm, err := Parse("/subscriptions/S/resourceGroups/RG/providers/Microsoft.Web/sites/app", "")
	if err != nil {
		t.Fatal(err)
	}
	portal, err := Parse("https://portal.azure.com/#@t/resource/subscriptions/s/resourcegroups/rg/providers/microsoft.web/sites/APP/overview", "")
	if err != nil {
		t.Fatal(err)
	}
	if arm.Key() != portal.Key() {
		t.Errorf("ARM key %q != portal key %q", arm.Key(), portal.Key())
	}

	first, _ := Parse("https://Example.com/a/", "")
	second, _ := Parse("HTTPS://example.com/a", "")
	if first.Key() != second.Key() {
		t.Errorf("link keys differ: %q vs %q", first.Key(), second.Key())
	}

	upper, _ := Parse("ab#7", "")
	lower, _ := Parse("AB#7", "")
	if upper.Key() != lower.Key() {
		t.Errorf("work item shorthand keys differ: %q vs %q", upper.Key(), lower.Key())
	}
}

func TestKeyDistinguishesDifferentReferences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		first, second Artifact
	}{
		{
			name:   "case-sensitive branch",
			first:  Artifact{Type: ADOBranch, Value: "https://dev.azure.com/o/p/_git/r?version=GBFeature"},
			second: Artifact{Type: ADOBranch, Value: "https://dev.azure.com/o/p/_git/r?version=GBfeature"},
		},
		{
			name:   "case-sensitive path",
			first:  Artifact{Type: Link, Value: "https://example.com/Doc"},
			second: Artifact{Type: Link, Value: "https://example.com/doc"},
		},
		{
			name:   "plain id in different repositories",
			first:  Artifact{Type: GitHubPullRequest, Value: "12", Repo: "api"},
			second: Artifact{Type: GitHubPullRequest, Value: "12", Repo: "web"},
		},
		{
			name:   "plain id of different types",
			first:  Artifact{Type: ADOBuild, Value: "9"},
			second: Artifact{Type: ADOWorkItem, Value: "9"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if test.first.Key() == test.second.Key() {
				t.Errorf("Key() collides for %+v and %+v", test.first, test.second)
			}
		})
	}
}

func TestURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		artifact Artifact
		want     string
	}{
		{"web URL", Artifact{Type: ADOBuild, Value: "https://dev.azure.com/org/Project/_build/results?buildId=123", ID: "123"}, "https://dev.azure.com/org/Project/_build/results?buildId=123"},
		{"link", Artifact{Type: Link, Value: "http://example.com/a b"}, "http://example.com/a%20b"},
		{"portal URL", Artifact{Type: AzureResource, Value: "https://portal.azure.com/#@tenant/resource/subscriptions/s/resourceGroups/rg/overview", ID: "/subscriptions/s/resourceGroups/rg"}, "https://portal.azure.com/#@tenant/resource/subscriptions/s/resourceGroups/rg/overview"},
		{"ARM id", Artifact{Type: AzureResource, Value: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Web/sites/app", ID: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Web/sites/app"}, "https://portal.azure.com/#resource/subscriptions/s/resourceGroups/rg/providers/Microsoft.Web/sites/app"},
		{"work item shorthand", Artifact{Type: ADOWorkItem, Value: "AB#4567", ID: "4567"}, ""},
		{"pull request shorthand", Artifact{Type: ADOPullRequest, Value: "!42", ID: "42"}, ""},
		{"plain id", Artifact{Type: ADOBuild, Value: "98765", ID: "98765"}, ""},
		{"ARM id with another type", Artifact{Type: Link, Value: "/subscriptions/s/resourceGroups/rg"}, ""},
		{"other scheme", Artifact{Type: Link, Value: "file:///etc/passwd"}, ""},
		{"control character", Artifact{Type: Link, Value: "https://example.com/\x1b]8;;evil\x07"}, ""},
		{"ARM id with control character", Artifact{Type: AzureResource, Value: "/subscriptions/s/resourceGroups/r\x1bg"}, "https://portal.azure.com/#resource/subscriptions/s/resourceGroups/r%1Bg"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := test.artifact.URL()
			if got != test.want || ok != (test.want != "") {
				t.Errorf("URL() = %q, %v; want %q", got, ok, test.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	if err := (Artifact{Type: Link, Value: "https://x"}).Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
	if err := (Artifact{Type: "bogus", Value: "x"}).Validate(); err == nil {
		t.Error("Validate() with unknown type error = nil")
	}
	if err := (Artifact{Type: Link}).Validate(); err == nil {
		t.Error("Validate() with empty value error = nil")
	}
}

func TestTypesReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()

	all := Types()
	if len(all) == 0 {
		t.Fatal("Types() returned empty slice")
	}
	first := all[0]
	all[0] = "mutated"
	if Types()[0] != first {
		t.Error("Types() did not return a defensive copy")
	}
	if got := decode("bad%ZZescape"); got != "bad%ZZescape" {
		t.Errorf("decode(invalid escape) = %q, want original segment", got)
	}
}

func FuzzParseAndInfer(f *testing.F) {
	for _, seed := range []struct {
		value  string
		forced string
	}{
		{"https://dev.azure.com/org/Proj/_build/results?buildId=123", ""},
		{"AB#4567", ""},
		{"!45", ""},
		{"/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg-app/providers/Microsoft.Storage/storageAccounts/stapp/overview", ""},
		{"https://github.com/o/r/pull/12", "link"},
		{"123", "ado-build"},
		{"https://portal.azure.com/#@t/resource/subscriptions/s/resourceGroups/rg", ""},
	} {
		f.Add(seed.value, seed.forced)
	}
	f.Fuzz(func(t *testing.T, value, forced string) {
		_, _, _ = Infer(value)
		got, err := Parse(value, Type(forced))
		if err != nil {
			return
		}
		if err := got.Validate(); err != nil {
			t.Fatalf("Parse(%q, %q) produced invalid Artifact %+v: %v", value, forced, got, err)
		}
		if key := got.Key(); key == "" || key != got.Key() {
			t.Fatalf("Key() for %+v = %q, want non-empty deterministic key", got, key)
		}
		if forced != "" && got.Type != Type(forced) {
			t.Fatalf("Parse(%q, %q) Type = %q, want %q", value, forced, got.Type, forced)
		}
	})
}
