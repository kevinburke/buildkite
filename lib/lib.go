package lib

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/kevinburke/go-types"
)

// The buildkite-go version. Run "make release" to bump this number.
const Version = "0.20"

type BuildState string

type Build struct {
	Number      int64          `json:"number"`
	State       BuildState     `json:"state"`
	Branch      string         `json:"branch"`
	Commit      string         `json:"commit"`
	Message     string         `json:"message"`
	WebURL      string         `json:"web_url"`
	CreatedAt   time.Time      `json:"created_at"`
	StartedAt   time.Time      `json:"started_at"`
	ScheduledAt types.NullTime `json:"scheduled_at"`
	FinishedAt  types.NullTime `json:"finished_at"`
	Jobs        []Job          `json:"jobs"`
	Pipeline    Pipeline       `json:"pipeline"`
	PullRequest *PullRequest   `json:"pull_request"`
}

type PullRequest struct {
	ID         string `json:"id"`
	Base       string `json:"base"`
	Repository string `json:"repository"`
}

func (p PullRequest) URL() string {
	u, err := url.Parse(p.Repository)
	if err != nil {
		return "%!ERROR"
	}
	return fmt.Sprintf("%s://%s%s/pull/%s", u.Scheme, u.Host, strings.TrimSuffix(u.Path, ".git"), p.ID)
}

type Pipeline struct {
	ID                   string    `json:"id"`
	Name                 string    `json:"name"`
	Slug                 string    `json:"slug"`
	CreatedAt            time.Time `json:"created_at"`
	RunningBuildsCount   int       `json:"running_builds_count"`
	ScheduledBuildsCount int       `json:"scheduled_builds_count"`
	RunningJobsCount     int       `json:"running_jobs_count"`
	WaitingJobsCount     int       `json:"waiting_jobs_count"`
	Repository           string    `json:"repository"`
}

type Job struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Command     string         `json:"command"`
	State       JobState       `json:"state"`
	CreatedAt   time.Time      `json:"created_at"`
	StartedAt   time.Time      `json:"started_at"`
	ScheduledAt types.NullTime `json:"scheduled_at"`
	FinishedAt  types.NullTime `json:"finished_at"`
	LogURL      string         `json:"log_url"`
}

type Log struct {
	Size    int64  `json:"size"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

func (j Job) Failed() bool {
	// TODO
	return j.State == "failed"
}

type JobState string

func (b Build) Empty() bool {
	return b.Number == 0
}

type ListBuildResponse []Build

type Annotation struct {
	ID        string    `json:"id"`
	Context   string    `json:"context"`
	BodyHTML  string    `json:"body_html"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AnnotationResponse []Annotation

type Organization struct {
	// This is the map key, so it needs to be explicitly set.
	Name  string
	Token string
	// List of git remotes that map to this Buildkite organization
	GitRemotes []string `toml:"git_remotes"`

	// The application name of the browser you would like to open for the "open"
	// command ("Google Chrome", "Firefox", "Chromium", etc).
	BrowserApplication string `toml:"browser_application"`

	// The profile name you would like to open. Chromium names these "Default",
	// "Profile 1", "Profile 2", etc. To find the exact name, look in the
	// "info_cache" field of e.g.
	// "~/Library/Application Support/Chromium/Local State".
	BrowserProfile string `toml:"browser_profile"`
}

// getCaseInsensitiveOrg finds the key in the list of orgs. This is a case
// insensitive match, so if key is "ExaMple" and orgs has a key named "eXAMPLE",
// that will count as a match.
func getCaseInsensitiveOrg(key string, orgs map[string]Organization) (Organization, bool) {
	for k := range orgs {
		lower := strings.ToLower(k)
		if _, ok := orgs[lower]; !ok {
			orgs[lower] = orgs[k]
			delete(orgs, k)
		}
	}
	lowerKey := strings.ToLower(key)
	if o, ok := orgs[lowerKey]; ok {
		return o, true
	} else {
		return Organization{}, false
	}
}

type FileConfig struct {
	Default string
	// Map key is the Buildkite name
	Organizations map[string]Organization `toml:"organizations"`
}

// LoadConfig loads and marshals a config file from disk. LoadConfig will look
// in the following locations in order:
//
// - $XDG_CONFIG_HOME/buildkite
// - $HOME/cfg/buildkite
// - $HOME/.buildkite
func LoadConfig(ctx context.Context) (*FileConfig, error) {

	var filename string
	var f *os.File
	var err error

	filename, err = getCfgPath()
	if err != nil {
		return nil, err
	}
	f, err = os.Open(filename)
	if err != nil {
		return nil, err
	}

	deadline, deadlineOk := ctx.Deadline()

	if deadlineOk && f != nil {
		f.SetDeadline(deadline)
	}
	defer f.Close()
	var c FileConfig

	if _, err := toml.NewDecoder(bufio.NewReader(f)).Decode(&c); err != nil {
		return nil, err
	}
	// set the name explicitly on each org, before parsing the config
	for i := range c.Organizations {
		entry := c.Organizations[i]
		entry.Name = i
		c.Organizations[i] = entry
	}
	return &c, nil
}

func (f *FileConfig) OrgForRemote(gitRemote string) (Organization, bool) {
	orgsByRemote := make(map[string]Organization)
	for _, org := range f.Organizations {
		for _, rm := range org.GitRemotes {
			orgsByRemote[rm] = org
		}
	}
	org, ok := orgsByRemote[gitRemote]
	return org, ok
}

// Token finds the token for a given git remote.
func (f *FileConfig) Token(gitRemote string) (string, error) {
	orgsByRemote := make(map[string]Organization)
	for _, org := range f.Organizations {
		for _, rm := range org.GitRemotes {
			orgsByRemote[rm] = org
		}
	}
	org, ok := getCaseInsensitiveOrg(gitRemote, orgsByRemote)
	if ok {
		return org.Token, nil
	}
	if f.Default != "" {
		defaultOrg, ok := getCaseInsensitiveOrg(f.Default, orgsByRemote)
		if ok {
			return defaultOrg.Token, nil
		}
		// try the other way too
		defaultOrg, ok = getCaseInsensitiveOrg(f.Default, f.Organizations)
		if ok {
			return defaultOrg.Token, nil
		}
		//lint:ignore ST1005 this shows up in public facing error.
		return "", fmt.Errorf(
			`Couldn't find an organization for git remote %s in the config.

Go to https://buildkite.com/user/api-access-tokens if you need to create or find a token.
		`, gitRemote)
	}
	//lint:ignore ST1005 this shows up in public facing error.
	return "", fmt.Errorf(
		`Couldn't find an organization for git remote %s in the config.

Set one of your organizations to be the default:

default = "kevinburke"

[organizations]

	[organizations.kevinburke-buildkite]
	token = "abcdef-bcd-fgh"

Or go to https://buildkite.com/user/api-access-tokens if you need to find your token.
		`, gitRemote)
}

func GetToken(ctx context.Context, org string) (string, error) {
	cfg, err := LoadConfig(ctx)
	if err != nil {
		return "", err
	}
	return cfg.Token(org)
}

var postCommandHookRe = regexp.MustCompile(`~~~ Running (global|local|plugin) post-command hook`)
var runCommandRe = regexp.MustCompile(`~~~ Running (global command|local command|plugin command|command|commands|script|batch script)\b`)

// FindBuildFailure will attempt to find the most "interesting" part of the log,
// based on heuristics. At most numOutputLines will be displayed.
func FindBuildFailure(log []byte, numOutputLines int) []byte {
	// We want to find the "end" of the "Running script" section, which can
	// contain an unknown number of tilde headers inside. I _believe_ the first
	// bit after this is the "Running global post-command hook" stanza. So we
	// seek to that and then read backwards.
	if len(log) == 0 {
		return log
	}
	idxMatch := postCommandHookRe.FindIndex(log)
	if idxMatch == nil {
		newlineIdx := 0
		for count := 0; count < numOutputLines; count++ {
			newlineIdx = newlineIdx + 1 + bytes.IndexByte(log[newlineIdx+1:], '\n')
			if newlineIdx == -1 {
				return log
			}
		}
		return log[:newlineIdx]
	}
	idx := idxMatch[0]
	// find the last N lines; stop when we get to "~~~ Running script"
	newlineIdx := idx
	for count := 0; count < numOutputLines; count++ {
		newIdx := bytes.LastIndexByte(log[:newlineIdx], '\n')
		if newIdx == -1 {
			return log[:idx]
		}
		if runCommandRe.Match(log[newIdx+1 : newlineIdx]) {
			break
		}
		newlineIdx = newIdx
	}
	if newlineIdx > idx {
		return log[:idx]
	}
	return log[newlineIdx:idx]
}
