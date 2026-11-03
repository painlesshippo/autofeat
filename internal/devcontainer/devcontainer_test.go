package devcontainer

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func identityConverter(path string) (string, error) { return path, nil }

func decodeURI(t *testing.T, uri string) (document devcontainerURIDocument, suffix string) {
	t.Helper()
	const prefix = "vscode-remote://dev-container+"
	if !strings.HasPrefix(uri, prefix) {
		t.Fatalf("URI = %q, want prefix %q", uri, prefix)
	}
	rest := strings.TrimPrefix(uri, prefix)
	end := strings.IndexFunc(rest, func(r rune) bool {
		return !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f'))
	})
	if end < 0 {
		t.Fatalf("URI = %q, want a path after the hex authority", uri)
	}
	decoded, err := hex.DecodeString(rest[:end])
	if err != nil {
		t.Fatalf("hex.DecodeString error = %v", err)
	}
	if err := json.Unmarshal(decoded, &document); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", decoded, err)
	}
	return document, rest[end:]
}

func TestBuildWorkspaceURILinux(t *testing.T) {
	featureDir := "/home/user/.autofeat-workspaces/feat"
	configPath := "/home/user/dev/service/.devcontainer/devcontainer.json"
	workspaceFile := featureDir + "/feat.code-workspace"

	uri, err := buildWorkspaceURI(featureDir, configPath, workspaceFile, identityConverter)
	if err != nil {
		t.Fatalf("buildWorkspaceURI() error = %v", err)
	}
	document, suffix := decodeURI(t, uri)
	// The workspace file opens from VS Code's default mount location for the
	// feature directory.
	if suffix != "/workspaces/feat/feat.code-workspace" {
		t.Errorf("container path = %q, want /workspaces/feat/feat.code-workspace", suffix)
	}
	if document.HostPath != featureDir {
		t.Errorf("hostPath = %q, want the feature directory %q", document.HostPath, featureDir)
	}
	if document.ConfigFile.Scheme != "file" || document.ConfigFile.Authority != "" || document.ConfigFile.Path != configPath {
		t.Errorf("configFile = %+v, want scheme file, empty authority, the external config path %q", document.ConfigFile, configPath)
	}
}

func TestBuildWorkspaceURIPercentEncodesReservedCharacters(t *testing.T) {
	featureDir := "/home/user/feat #1"
	uri, err := buildWorkspaceURI(featureDir, featureDir+"/dc.json", featureDir+"/a b?c.code-workspace", identityConverter)
	if err != nil {
		t.Fatalf("buildWorkspaceURI() error = %v", err)
	}
	_, suffix := decodeURI(t, uri)
	if strings.ContainsAny(suffix, " #?") {
		t.Errorf("container path = %q, want no raw reserved characters", suffix)
	}
	if suffix != "/workspaces/feat%20%231/a%20b%3Fc.code-workspace" {
		t.Errorf("container path = %q, want reserved characters percent-encoded", suffix)
	}
}

func TestBuildWorkspaceURIWSLRepresentation(t *testing.T) {
	featureDir := "/home/alix/feat"
	configPath := "/home/alix/dev/service/.devcontainer/devcontainer.json"
	workspaceFile := featureDir + "/feat.code-workspace"
	converter := func(path string) (string, error) {
		return `\\wsl.localhost\Debian` + strings.ReplaceAll(path, "/", `\`), nil
	}

	uri, err := buildWorkspaceURI(featureDir, configPath, workspaceFile, converter)
	if err != nil {
		t.Fatalf("buildWorkspaceURI() error = %v", err)
	}
	document, suffix := decodeURI(t, uri)
	if suffix != "/workspaces/feat/feat.code-workspace" {
		t.Errorf("container path = %q, want the unconverted Linux container path", suffix)
	}
	if document.HostPath != `\\wsl.localhost\Debian\home\alix\feat` {
		t.Errorf("hostPath = %q, want the Windows UNC path", document.HostPath)
	}
	if document.ConfigFile.Authority != "wsl.localhost" {
		t.Errorf("configFile.authority = %q, want wsl.localhost", document.ConfigFile.Authority)
	}
	if document.ConfigFile.Path != "/Debian/home/alix/dev/service/.devcontainer/devcontainer.json" {
		t.Errorf("configFile.path = %q, want the distro-rooted external config path", document.ConfigFile.Path)
	}
}

func TestBuildWorkspaceURIRejectsWorkspaceOutsideFeatureDir(t *testing.T) {
	if _, err := buildWorkspaceURI("/home/user/feat", "/home/user/dc.json", "/home/user/other/feat.code-workspace", identityConverter); err == nil {
		t.Fatal("buildWorkspaceURI() error = nil, want workspace-outside-feature-dir error")
	}
}

func TestBuildWorkspaceURIConverterErrorsAndHelpers(t *testing.T) {
	// Converter failure on featureDir (1st call) and configPath (2nd call).
	calls := 0
	failFirst := func(string) (string, error) {
		return "", errors.New("convert failed")
	}
	if _, err := buildWorkspaceURI("/home/user/feat", "/home/user/dc.json", "/home/user/feat/feat.code-workspace", failFirst); err == nil {
		t.Error("buildWorkspaceURI(fail first convert) error = nil, want error")
	}
	failSecond := func(path string) (string, error) {
		calls++
		if calls == 2 {
			return "", errors.New("convert config failed")
		}
		return path, nil
	}
	if _, err := buildWorkspaceURI("/home/user/feat", "/home/user/dc.json", "/home/user/feat/feat.code-workspace", failSecond); err == nil {
		t.Error("buildWorkspaceURI(fail second convert) error = nil, want error")
	}

	if auth, path := fileURIParts(`\\wsl.localhost`); auth != "wsl.localhost" || path != "/" {
		t.Errorf("fileURIParts(UNC root) = (%q, %q), want (wsl.localhost, /)", auth, path)
	}
	if auth, path := fileURIParts(`C:\Users\me\dc.json`); auth != "" || path != "/C:/Users/me/dc.json" {
		t.Errorf("fileURIParts(drive path) = (%q, %q), want (\"\", /C:/Users/me/dc.json)", auth, path)
	}
	if got := escapeURIPath("workspaces/feat"); got != "/workspaces/feat" {
		t.Errorf("escapeURIPath(relative) = %q, want /workspaces/feat", got)
	}

	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	if !runningUnderWSL() {
		t.Error("runningUnderWSL() with WSL_DISTRO_NAME = false, want true")
	}

	// Stub wslpath on PATH so WorkspaceURI and wslWindowsPath are deterministically tested.
	binDir := t.TempDir()
	wslpathScript := filepath.Join(binDir, "wslpath")
	if err := os.WriteFile(wslpathScript, []byte("#!/bin/sh\nprintf '\\\\\\\\wsl.localhost\\\\Ubuntu%s\\r\\n' \"$(printf '%s' \"$2\" | tr '/' '\\\\')\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	uri, err := WorkspaceURI("/home/user/feat", "/home/user/dc.json", "/home/user/feat/feat.code-workspace")
	if err != nil {
		t.Fatalf("WorkspaceURI() error = %v", err)
	}
	doc, suffix := decodeURI(t, uri)
	if suffix != "/workspaces/feat/feat.code-workspace" || doc.ConfigFile.Authority != "wsl.localhost" {
		t.Errorf("WorkspaceURI() = (%+v, %q)", doc, suffix)
	}

	if err := os.WriteFile(wslpathScript, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := wslWindowsPath("/home/user/feat"); err == nil {
		t.Error("wslWindowsPath(failing wslpath) error = nil, want error")
	}
}

func FuzzBuildWorkspaceURI(f *testing.F) {
	f.Add("feat.code-workspace", "/home/user/dc.json")
	f.Add("sub/a b?c#1.code-workspace", "/home/user/my#dc?.json")
	f.Fuzz(func(t *testing.T, relWorkspace, configPath string) {
		featureDir := "/home/user/feat"
		workspaceFile := filepath.Join(featureDir, relWorkspace)
		uri, err := buildWorkspaceURI(featureDir, filepath.Join("/home/user", configPath), workspaceFile, identityConverter)
		if err != nil {
			return
		}
		doc, suffix := decodeURI(t, uri)
		if doc.HostPath != featureDir {
			t.Fatalf("HostPath = %q, want %q", doc.HostPath, featureDir)
		}
		if !strings.HasPrefix(suffix, "/workspaces/") || strings.ContainsAny(suffix, " #?") {
			t.Fatalf("suffix = %q, want /workspaces/ prefix and no raw reserved chars", suffix)
		}
	})
}
