package lib

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/kevinburke/rest/restclient"
	"github.com/kevinburke/rest/resterror"
)

// The buildkite-go version. Run "make release" to bump this number.
const Version = "0.1"
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
	Number  int64      `json:"number"`
	State   BuildState `json:"state"`
	Branch  string     `json:"branch"`
	Commit  string     `json:"commit"`
	Message string     `json:"message"`
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

func GetToken(org string) (string, error) {
	return os.Getenv("BUILDKITE_TOKEN"), nil
}

func NewClient(token string) *Client {
	host := getHost()
	if host == "" {
		host = Host
	}
	rc := restclient.NewBearerClient(token, host)
	return &Client{Client: rc}
}
