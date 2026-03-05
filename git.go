package main

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

// RemoteURL holds parsed components of a git remote URL.
type RemoteURL struct {
	// Path is the user or organization (e.g. "kevinburke" in github.com/kevinburke/buildkite)
	Path string
	// RepoName is the repository name (e.g. "buildkite")
	RepoName string
}

// getRemoteURL returns a parsed RemoteURL for the named git remote.
func getRemoteURL(remoteName string) (*RemoteURL, error) {
	out, err := exec.Command("git", "config", "--get", fmt.Sprintf("remote.%s.url", remoteName)).Output()
	if err != nil {
		return nil, fmt.Errorf("getting remote %q URL: %w", remoteName, err)
	}
	return parseRemoteURL(strings.TrimSpace(string(out)))
}

// parseRemoteURL extracts the org/user and repo name from a git remote URL.
// Supports:
//   - SSH short form: git@github.com:user/repo.git
//   - HTTPS: https://github.com/user/repo.git
//   - SSH long form: ssh://git@github.com/user/repo.git
func parseRemoteURL(raw string) (*RemoteURL, error) {
	raw = strings.TrimSpace(raw)

	// SSH short form: git@host:path/repo.git
	if i := strings.Index(raw, "@"); i >= 0 && !strings.Contains(raw, "://") {
		// Everything after the ":"
		colonIdx := strings.Index(raw[i:], ":")
		if colonIdx < 0 {
			return nil, fmt.Errorf("could not parse git remote URL %q", raw)
		}
		pathRepo := raw[i+colonIdx+1:]
		return splitPathRepo(pathRepo, raw)
	}

	// URL form (https://, ssh://, etc.)
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("could not parse git remote URL %q: %w", raw, err)
	}
	pathRepo := strings.TrimPrefix(u.Path, "/")
	return splitPathRepo(pathRepo, raw)
}

func splitPathRepo(pathRepo, raw string) (*RemoteURL, error) {
	pathRepo = strings.TrimSuffix(pathRepo, "/")
	pathRepo = strings.TrimSuffix(pathRepo, ".git")
	parts := strings.Split(pathRepo, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("could not parse git remote URL %q: expected user/repo path, got %q", raw, pathRepo)
	}
	return &RemoteURL{
		Path:     strings.Join(parts[:len(parts)-1], "/"),
		RepoName: parts[len(parts)-1],
	}, nil
}

// currentBranch returns the name of the current Git branch.
func currentBranch(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("getting current branch: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// gitTip returns the full commit SHA for the given branch or ref.
// If branch is empty, defaults to HEAD.
func gitTip(branch string) (string, error) {
	if branch == "" {
		branch = "HEAD"
	}
	out, err := exec.Command("git", "rev-parse", branch).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("getting tip of %q: %s", branch, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// getBranchFromArgs returns the git branch from args, or the current branch if
// no argument is specified.
func getBranchFromArgs(ctx context.Context, args []string) (string, error) {
	if len(args) == 0 {
		return currentBranch(ctx)
	}
	return args[0], nil
}
