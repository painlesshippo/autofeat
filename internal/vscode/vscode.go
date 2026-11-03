// Package vscode detects whether a workspace file is open in a running VS Code
// window.
//
// Each VS Code window's extension host holds a lock file at
// <userData>/User/workspaceStorage/<workspaceID>/vscode.lock, refreshes its
// modification time every second while the window is open, and deletes it on
// exit. A workspace file's ID is the MD5 of its path, or of its
// vscode-remote URI for a Remote-WSL window, lowercased when the VS Code
// client does not run on Linux. A recently refreshed lock therefore means the
// workspace is open.
//
// Detection is best-effort: it covers local and Remote-WSL windows of stable
// VS Code, not Dev Container or SSH windows, whose extension hosts run on
// another machine.
package vscode

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// lockFreshness is how recently a lock must have been refreshed for its
// window to count as open. VS Code refreshes it every second.
const lockFreshness = 10 * time.Second

// WorkspaceOpen reports whether workspaceFile is open in a VS Code window.
func WorkspaceOpen(workspaceFile string) bool {
	return workspaceOpen(workspaceFile, storageDirectories(), os.Getenv("WSL_DISTRO_NAME"), time.Now())
}

func storageDirectories() []string {
	var directories []string
	if configDir, err := os.UserConfigDir(); err == nil {
		directories = append(directories, filepath.Join(configDir, "Code", "User", "workspaceStorage"))
	}
	if homeDir, err := os.UserHomeDir(); err == nil {
		directories = append(directories, filepath.Join(homeDir, ".vscode-server", "data", "User", "workspaceStorage"))
	}
	return directories
}

func workspaceOpen(workspaceFile string, storageDirs []string, wslDistro string, now time.Time) bool {
	if strings.TrimSpace(workspaceFile) == "" {
		return false
	}
	for _, directory := range storageDirs {
		for _, id := range workspaceIDs(workspaceFile, wslDistro) {
			info, err := os.Stat(filepath.Join(directory, id, "vscode.lock"))
			if err == nil && now.Sub(info.ModTime()) < lockFreshness {
				return true
			}
		}
	}
	return false
}

// workspaceIDs returns the IDs VS Code may assign workspaceFile: its path as
// seen by a Linux client and by a macOS or Windows client, plus its
// Remote-WSL URI when wslDistro is set.
func workspaceIDs(workspaceFile, wslDistro string) []string {
	candidates := []string{workspaceFile, strings.ToLower(workspaceFile)}
	if wslDistro != "" {
		uri := "vscode-remote://wsl%2B" + escapeURIComponent(wslDistro, false) + escapeURIComponent(filepath.ToSlash(workspaceFile), true)
		candidates = append(candidates, strings.ToLower(uri))
	}
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		sum := md5.Sum([]byte(candidate))
		ids = append(ids, hex.EncodeToString(sum[:]))
	}
	return ids
}

// escapeURIComponent percent-encodes text the way VS Code's URI.toString
// does: unreserved characters, and '/' within a path, stay as they are.
func escapeURIComponent(text string, isPath bool) string {
	const hexDigits = "0123456789ABCDEF"
	var builder strings.Builder
	for _, b := range []byte(text) {
		switch {
		case 'a' <= b && b <= 'z', 'A' <= b && b <= 'Z', '0' <= b && b <= '9',
			b == '-', b == '.', b == '_', b == '~', isPath && b == '/':
			builder.WriteByte(b)
		default:
			builder.WriteByte('%')
			builder.WriteByte(hexDigits[b>>4])
			builder.WriteByte(hexDigits[b&0x0f])
		}
	}
	return builder.String()
}
