// Package devcontainer builds the vscode-remote URI that opens a feature
// session's generated multi-root workspace using a developer-provided
// devcontainer.json.
//
// autofeat treats that configuration as opaque: it neither parses, copies, nor
// modifies it. The configuration owns the container's mounts, its handling of
// linked-worktree Git metadata, its localWorkspaceFolder semantics, and its
// compatibility with the session layout. autofeat only encodes the paths into
// the URI the Dev Containers extension understands:
//
//	code --file-uri "vscode-remote://dev-container+<hex><containerPath>"
//
// where <hex> encodes the feature directory VS Code opens as the container's
// local workspace folder and the developer's configuration file, and
// <containerPath> is the percent-encoded path of the workspace file inside the
// container. The workspace file is opened from VS Code's default mount location
// for the local workspace folder, /workspaces/<feature-dir-name>/<file>.
package devcontainer

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// WorkspaceURI builds the vscode-remote://dev-container+ file URI that opens
// workspaceFile inside the container defined by configPath. featureDir is the
// directory that holds the workspace file and is opened as the container's local
// workspace folder; workspaceFile must live within it. configPath is the
// developer-provided devcontainer.json, passed through untouched.
func WorkspaceURI(featureDir, configPath, workspaceFile string) (string, error) {
	return buildWorkspaceURI(featureDir, configPath, workspaceFile, defaultHostConverter())
}

// buildWorkspaceURI is the testable core of WorkspaceURI. convert maps an
// absolute local path to the host representation VS Code expects (an identity on
// Linux, or a Windows path under WSL).
func buildWorkspaceURI(featureDir, configPath, workspaceFile string, convert hostPathConverter) (string, error) {
	featureDir = filepath.Clean(featureDir)
	workspaceFile = filepath.Clean(workspaceFile)

	relative, err := filepath.Rel(featureDir, workspaceFile)
	if err != nil {
		return "", fmt.Errorf("locate workspace file within %q: %w", featureDir, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workspace file %q is not inside the feature directory %q", workspaceFile, featureDir)
	}
	containerPath := path.Join("/workspaces/"+filepath.Base(featureDir), filepath.ToSlash(relative))

	hostPathRepresentation, err := convert(featureDir)
	if err != nil {
		return "", err
	}
	configRepresentation, err := convert(filepath.Clean(configPath))
	if err != nil {
		return "", err
	}
	authority, configFilePath := fileURIParts(configRepresentation)

	document := devcontainerURIDocument{HostPath: hostPathRepresentation}
	document.ConfigFile.Scheme = "file"
	document.ConfigFile.Authority = authority
	document.ConfigFile.Path = configFilePath

	encoded, err := json.Marshal(document)
	if err != nil {
		return "", fmt.Errorf("encode dev container URI: %w", err)
	}

	return "vscode-remote://dev-container+" + hex.EncodeToString(encoded) + escapeURIPath(containerPath), nil
}

type devcontainerURIDocument struct {
	HostPath   string `json:"hostPath"`
	ConfigFile struct {
		Scheme    string `json:"scheme"`
		Authority string `json:"authority,omitempty"`
		Path      string `json:"path"`
	} `json:"configFile"`
}

// hostPathConverter maps an absolute local path to the host-path representation
// VS Code expects in a dev-container URI.
type hostPathConverter func(localPath string) (string, error)

func defaultHostConverter() hostPathConverter {
	if runningUnderWSL() {
		return wslWindowsPath
	}
	return func(localPath string) (string, error) { return localPath, nil }
}

func runningUnderWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	release, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return false
	}
	lowered := strings.ToLower(string(release))
	return strings.Contains(lowered, "microsoft") || strings.Contains(lowered, "wsl")
}

// wslWindowsPath converts a WSL Linux path to the Windows path the Dev Containers
// extension on the Windows host uses to locate the folder.
func wslWindowsPath(localPath string) (string, error) {
	output, err := exec.Command("wslpath", "-w", localPath).Output()
	if err != nil {
		return "", fmt.Errorf("translate WSL path %q with wslpath: %w", localPath, err)
	}
	return strings.TrimRight(string(output), "\r\n"), nil
}

// fileURIParts splits a host path representation into the authority and path of a
// file URI. A Windows UNC path such as \\wsl.localhost\Debian\home yields the
// authority "wsl.localhost"; a plain absolute path yields an empty authority.
func fileURIParts(hostPath string) (authority, uriPath string) {
	slashed := strings.ReplaceAll(hostPath, "\\", "/")
	if strings.HasPrefix(slashed, "//") {
		rest := strings.TrimPrefix(slashed, "//")
		if index := strings.IndexByte(rest, '/'); index >= 0 {
			return rest[:index], rest[index:]
		}
		return rest, "/"
	}
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return "", slashed
}

// escapeURIPath percent-encodes a container path so that reserved characters such
// as '#', '?' and '%' stay part of the path instead of altering the URI.
func escapeURIPath(containerPath string) string {
	escaped := (&url.URL{Path: containerPath}).EscapedPath()
	if !strings.HasPrefix(escaped, "/") {
		escaped = "/" + escaped
	}
	return escaped
}
