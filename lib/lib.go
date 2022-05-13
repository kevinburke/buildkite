package lib

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/kevinburke/go-types"
	"github.com/kevinburke/rest/restclient"
	"github.com/kevinburke/rest/resterror"
)

// The buildkite-go version. Run "make release" to bump this number.
const Version = "0.2"
const userAgent = "buildkite-go/" + Version

const APIVersion = "v2"

type Client struct {
	*restclient.Client
	APIVersion string
}

// GetResource retrieves an instance resource with the given path part (e.g.
// "/Messages") and sid (e.g. "MM123").
func (c *Client) GetResource(ctx context.Context, pathPart string, sid string, v interface{}) error {
	sidPart := strings.Join([]string{pathPart, sid}, "/")
	return c.MakeRequest(ctx, "GET", sidPart, nil, v)
}

// CreateResource makes a POST request to the given resource.
func (c *Client) CreateResource(ctx context.Context, pathPart string, data url.Values, v interface{}) error {
	return c.MakeRequest(ctx, "POST", pathPart, data, v)
}

func (c *Client) UpdateResource(ctx context.Context, pathPart string, sid string, data url.Values, v interface{}) error {
	sidPart := strings.Join([]string{pathPart, sid}, "/")
	return c.MakeRequest(ctx, "POST", sidPart, data, v)
}

func (c *Client) DeleteResource(ctx context.Context, pathPart string, sid string) error {
	sidPart := strings.Join([]string{pathPart, sid}, "/")
	err := c.MakeRequest(ctx, "DELETE", sidPart, nil, nil)
	if err == nil {
		return nil
	}
	rerr, ok := err.(*resterror.Error)
	if ok && rerr.Status == http.StatusNotFound {
		return nil
	}
	return err
}

func (c *Client) MakeRequest(ctx context.Context, method string, pathPart string, data url.Values, v interface{}) error {
	rb := new(strings.Reader)
	if data != nil && (method == "POST" || method == "PUT") {
		rb = strings.NewReader(data.Encode())
	}
	if method == "GET" && data != nil {
		pathPart = pathPart + "?" + data.Encode()
	}
	req, err := c.NewRequest(method, "/"+APIVersion+pathPart, rb)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)
	if ua := req.Header.Get("User-Agent"); ua == "" {
		req.Header.Set("User-Agent", userAgent)
	} else {
		req.Header.Set("User-Agent", userAgent+" "+ua)
	}
	return c.Do(req, &v)
}

func (c *Client) ListResource(ctx context.Context, pathPart string, data url.Values, v interface{}) error {
	return c.MakeRequest(ctx, "GET", pathPart, data, v)
}

type OrganizationService struct {
	client *Client
	org    string
}

type PipelineService struct {
	client   *Client
	org      string
	pipeline string
}

func (o *OrganizationService) Pipeline(pipeline string) *PipelineService {
	return &PipelineService{
		client:   o.client,
		org:      o.org,
		pipeline: pipeline,
	}
}

type BuildState string

type Build struct {
	Number      int64          `json:"number"`
	State       BuildState     `json:"state"`
	Branch      string         `json:"branch"`
	Commit      string         `json:"commit"`
	Message     string         `json:"message"`
	WebURL      string         `json:"web_url"`
	LogURL      string         `json:"log_url"`
	CreatedAt   time.Time      `json:"created_at"`
	StartedAt   time.Time      `json:"started_at"`
	ScheduledAt types.NullTime `json:"scheduled_at"`
	FinishedAt  types.NullTime `json:"finished_at"`
}

func (b Build) Empty() bool {
	return b.Number == 0
}

type ListBuildResponse []Build

func (p *PipelineService) ListBuilds(ctx context.Context, query url.Values) (ListBuildResponse, error) {
	path := "/organizations/" + p.org + "/pipelines/" + p.pipeline + "/builds"
	var val ListBuildResponse
	err := p.client.ListResource(ctx, path, query, &val)
	return val, err
}

func (c *Client) Organization(org string) *OrganizationService {
	return &OrganizationService{client: c, org: org}
}

const Host = "https://api.buildkite.com"

func getHost() string {
	return Host
}

type Organization struct {
	// This is the map key, so it needs to be explicitly set.
	Name  string
	Token string
	// List of git remotes that map to this Buildkite organization
	GitRemotes []string `toml:"git_remotes"`
}

// getCaseInsensitiveOrg finds the key in the list of orgs. This is a case
// insensitive match, so if key is "ExaMple" and orgs has a key named "eXAMPLE",
// that will count as a match.
func getCaseInsensitiveOrg(key string, orgs map[string]Organization) (Organization, bool) {
	for k, _ := range orgs {
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
	checkedLocations := make([]string, 1)
	deadline, deadlineOk := ctx.Deadline()
	if cfg, ok := os.LookupEnv("XDG_CONFIG_HOME"); ok {
		filename = filepath.Join(cfg, "buildkite")
		f, err = os.Open(filename)
		checkedLocations[0] = filename
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		filename = filepath.Join(homeDir, "cfg", "buildkite")
		f, err = os.Open(filename)
		checkedLocations[0] = filename
		if err != nil { // fallback
			rcFilename := filepath.Join(homeDir, ".buildkite")
			f, err = os.Open(rcFilename)
			checkedLocations = append(checkedLocations, rcFilename)
		}
	}
	if deadlineOk {
		f.SetDeadline(deadline)
	}
	if err != nil {
		err = fmt.Errorf(`Couldn't find a config file in %s.

Add a configuration file with your Buildkite token, like this:

[organizations]

    [organizations.buildkite_org]
    token = "aabbccddeeff00"
    git_remote = "github_org"

Go to https://buildkite.com/user/api-access-tokens if you need to find your token.
`, strings.Join(checkedLocations, " or "))
		return nil, err
	}
	defer f.Close()
	var c FileConfig
	if _, err := toml.DecodeReader(bufio.NewReader(f), &c); err != nil {
		return nil, err
	}
	// set the name explicitly
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
		return "", fmt.Errorf(
			`Couldn't find an organization for git remote %s in the config.

Go to https://buildkite.com/user/api-access-tokens if you need to create or find a token.
		`, gitRemote)
	}
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

func NewClient(token string) *Client {
	host := getHost()
	if host == "" {
		host = Host
	}
	rc := restclient.NewBearerClient(token, host)
	return &Client{Client: rc}
}
