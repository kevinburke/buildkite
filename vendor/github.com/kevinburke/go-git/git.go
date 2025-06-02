package git

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const version = "0.11.1"

type GitFormat int

const (
	SSHFormat   GitFormat = iota
	HTTPSFormat           = iota
)

// Short ssh style doesn't allow a custom port
// http://stackoverflow.com/a/5738592/329700
var sshExp = regexp.MustCompile(`^(?P<sshUser>[^@]+)@(?P<domain>[^:]+):(?P<pathRepo>.*)(\.git/?)?$`)

// https://github.com/kevinburke/go-circle.git
var httpsExp = regexp.MustCompile(`^https://(?P<domain>[^/:]+)(:(?P<port>[[0-9]+))?/(?P<pathRepo>.+?)(\.git/?)?$`)

// A remote URL. Easiest to describe with an example:
//
// git@github.com:kevinburke/go-circle.git
//
// Would be parsed as follows:
//
// Path     = kevinburke
// Host     = github.com
// RepoName = go-circle
// SSHUser  = git
// URL      = git@github.com:kevinburke/go-circle.git
// Format   = SSHFormat
//
// Similarly:
//
// https://github.com/kevinburke/go-circle.git
//
// User     = kevinburke
// Host     = github.com
// RepoName = go-circle
// SSHUser  = ""
// Format   = HTTPSFormat
type RemoteURL struct {
	Host     string
	Port     int
	Path     string
	RepoName string
	Format   GitFormat

	// The full URL
	URL string

	// If the remote uses the SSH format, this is the name of the SSH user for
	// the remote. Usually "git@"
	SSHUser string
}

func getPathAndRepoName(pathAndRepo string) (string, string) {
	pathAndRepo = strings.TrimSuffix(pathAndRepo, "/")
	paths := strings.Split(pathAndRepo, "/")
	repoName := paths[len(paths)-1]
	path := strings.Join(paths[:len(paths)-1], "/")

	repoName = strings.TrimSuffix(repoName, ".git")
	return path, repoName
}

// ParseRemoteURL takes a git remote URL and returns an object with its
// component parts, or an error if the remote cannot be parsed
func ParseRemoteURL(remoteURL string) (*RemoteURL, error) {
	remoteURL = strings.TrimSpace(remoteURL)
	match := sshExp.FindStringSubmatch(remoteURL)
	if len(match) > 0 {
		path, repoName := getPathAndRepoName(match[3])
		return &RemoteURL{
			Path:     path,
			Host:     match[2],
			RepoName: repoName,
			URL:      match[0],
			Port:     22,

			Format:  SSHFormat,
			SSHUser: match[1],
		}, nil
	}
	match = httpsExp.FindStringSubmatch(remoteURL)
	if len(match) > 0 {
		var port int
		var err error
		if len(match[3]) > 0 {
			port, err = strconv.Atoi(match[3])
			if err != nil {
				log.Panicf("git: invalid port: %s", match[3])
			}
		} else {
			port = 443
		}
		path, repoName := getPathAndRepoName(match[4])
		return &RemoteURL{
			Path:     path,
			Host:     match[1],
			RepoName: repoName,
			URL:      match[0],
			Port:     port,

			Format: HTTPSFormat,
		}, nil
	}
	u, err := url.Parse(remoteURL)
	if err != nil {
		return nil, fmt.Errorf("could not parse %s as a git remote", remoteURL)
	}
	if u.Scheme == "ssh" {
		// long SSH format
		rmurl := &RemoteURL{
			Format:  SSHFormat,
			SSHUser: u.User.Username(),
			Host:    u.Hostname(),
			URL:     remoteURL,
		}
		portstr := u.Port()
		if portstr == "" {
			rmurl.Port = 22
		} else {
			port, err := strconv.Atoi(portstr)
			if err != nil && port < 0 {
				return nil, fmt.Errorf("could not parse %s as a git remote: port was not a valid number", remoteURL)
			}
			rmurl.Port = port
		}
		path := strings.TrimPrefix(u.Path, "/")
		pathparts := strings.Split(path, "/")
		// this might be too "tight" but in practice this is how remote URL's
		// are formed at most every git repo
		if len(pathparts) > 2 {
			return nil, fmt.Errorf("could not parse %s as a git remote: too many slashes in repo name", remoteURL)
		}
		if len(pathparts) <= 1 {
			return nil, fmt.Errorf("could not parse %s as a git remote: could not find both org and repo", remoteURL)
		}
		rmurl.Path = pathparts[0]
		rmurl.RepoName = strings.TrimSuffix(pathparts[1], ".git")
		return rmurl, nil
	}
	return nil, fmt.Errorf("could not parse %s as a git remote", remoteURL)
}

// RemoteURL returns a Remote object with information about the given Git
// remote.
func GetRemoteURL(remoteName string) (*RemoteURL, error) {
	rawRemote, err := exec.Command("git", "config", "--get", fmt.Sprintf("remote.%s.url", remoteName)).Output()
	if err != nil {
		return nil, err
	}
	// git response includes a newline
	remote := strings.TrimSpace(string(rawRemote))
	return ParseRemoteURL(remote)
}

// CurrentBranch returns the name of the current Git branch. Returns an error
// if you are not on a branch, or if you are not in a git repository.
func CurrentBranch(ctx context.Context) (string, error) {
	result, err := exec.CommandContext(ctx, "git", "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		return "", err
	}

	// Strip any prefix (heads/, remotes/origin/, tags/, etc.)
	branch := strings.TrimSpace(string(result))
	if idx := strings.LastIndex(branch, "/"); idx != -1 {
		branch = branch[idx+1:]
	}

	return branch, nil
}

// Tip returns the SHA of the given Git branch. If the empty string is
// provided, defaults to HEAD on the current branch.
func Tip(branch string) (string, error) {
	if branch == "" {
		branch = "HEAD"
	}
	result, err := exec.Command("git", "rev-parse", branch).CombinedOutput()
	if err != nil {
		if strings.Contains(string(result), "Needed a single revision") {
			return "", fmt.Errorf("git: Branch %s is unknown, can't get tip", branch)
		}
		return "", fmt.Errorf("error getting tip for branch %q: %v (message %s)", branch, err, string(result))
	}
	return strings.TrimSpace(string(result)), nil
}

// Tip returns a short (usually 6 to 8 byte) SHA of the given Git branch. If
// branch is empty, defaults to HEAD on the current branch.
func ShortTip(branch string) (string, error) {
	if branch == "" {
		branch = "HEAD"
	}
	result, err := exec.Command("git", "rev-parse", "--short", branch).CombinedOutput()
	if err != nil {
		if strings.Contains(string(result), "Needed a single revision") {
			return "", fmt.Errorf("git: Branch %s is unknown, can't get tip", branch)
		}
		return "", err
	}
	return strings.TrimSpace(string(result)), nil
}

// Root returns the root directory of the current Git repository, or an error
// if you are not in a git repository. If directory is not the empty string,
// change the working directory before running the command.
func Root(directory string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = filepath.Dir(directory)
	result, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(result))
	if err != nil {
		return "", errors.New(trimmed)
	}
	return strings.TrimSpace(trimmed), nil
}
