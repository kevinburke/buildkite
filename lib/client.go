package lib

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/kevinburke/rest/restclient"
	"github.com/kevinburke/rest/resterror"
	"golang.org/x/term"
)

const userAgent = "buildkite-go/" + Version

const APIVersion = "v2"

type Error struct {
	StatusCode int
	Message    string
}

type buildkiteErrorResponse struct {
	Message string `json:"message"`
}

func (b *Error) Error() string {
	return b.Message
}

func NewClient(token string) *Client {
	host := getHost()
	if host == "" {
		host = Host
	}
	rc := restclient.NewBearerClient(token, host)
	rc.ErrorParser = func(r *http.Response) error {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			return fmt.Errorf("received HTTP error %d from Buildkite and could not read the response: %v", r.StatusCode, err)
		}
		resp := new(buildkiteErrorResponse)
		if err := json.Unmarshal(data, &resp); err != nil {
			return fmt.Errorf("could not decode %d error response as a Buildkite error: %w", r.StatusCode, err)
		}
		return &Error{Message: resp.Message, StatusCode: r.StatusCode}
	}

	qc := restclient.NewBearerClient(token, GraphQLHost)
	qc.ErrorParser = func(r *http.Response) error {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			return fmt.Errorf("received HTTP error %d from Buildkite and could not read the response: %v", r.StatusCode, err)
		}
		resp := new(buildkiteErrorResponse)
		if err := json.Unmarshal(data, &resp); err != nil {
			return fmt.Errorf("could not decode %d error response as a Buildkite error: %w", r.StatusCode, err)
		}
		return &Error{Message: resp.Message, StatusCode: r.StatusCode}
	}

	return &Client{
		Client:        rc,
		GraphQLClient: qc,
	}
}

type Client struct {
	*restclient.Client
	GraphQLClient *restclient.Client
	APIVersion    string
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
	req, err := c.NewRequestWithContext(ctx, method, "/"+APIVersion+pathPart, rb)
	if err != nil {
		return err
	}
	if ua := req.Header.Get("User-Agent"); ua == "" {
		req.Header.Set("User-Agent", userAgent)
	} else {
		req.Header.Set("User-Agent", userAgent+" "+ua)
	}
	return c.Do(req, &v)
}

func (c *Client) GraphQLRequest(ctx context.Context, query string, v interface{}) error {
	req, err := c.GraphQLClient.NewRequestWithContext(ctx, "POST", "/v1", strings.NewReader(query))
	if err != nil {
		return err
	}
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

type canQueryData struct {
	Typename string `json:"__typename"`
}

type canQueryResult struct {
	Data canQueryData `json:"data"`
}

func (c *GraphQLService) Can(ctx context.Context) (bool, error) {
	query := `{"query":"{ __typename }"}`
	var v canQueryResult
	if err := c.client.GraphQLRequest(ctx, query, &v); err != nil {
		return false, err
	}
	return v.Data.Typename != "", nil
}

type GraphQLService struct {
	client *Client
}

func (c *Client) GraphQL() *GraphQLService {
	return &GraphQLService{c}
}

type PipelineRepositoriesSlugsResponse struct {
	Data struct {
		Organization struct {
			Pipelines Pipelines `json:"pipelines"`
		} `json:"organization"`
	} `json:"data"`
}

// Pipelines groups the edges (actual pipelines) and page-info.
type Pipelines struct {
	PageInfo PageInfo       `json:"pageInfo"`
	Edges    []PipelineEdge `json:"edges"`
}

// PageInfo carries pagination cursors / flags.
type PageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

// PipelineEdge wraps the Node, exactly like the GraphQL connection spec.
type PipelineEdge struct {
	Node PipelineNode `json:"node"`
}

// PipelineNode is a single Buildkite pipeline.
type PipelineNode struct {
	Slug       string     `json:"slug"`
	Repository Repository `json:"repository"`
	Builds     Builds     `json:"builds"`
}

type Builds struct {
	Edges []BuildEdge `json:"edges"`
}

type BuildEdge struct {
	Node BuildNode `json:"node"`
}

type BuildNode struct {
	CreatedAt time.Time `json:"createdAt"`
}

// Repository is the Git repository backing the pipeline.
type Repository struct {
	URL string `json:"url"`
}

type GraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

func (c *GraphQLService) PipelineRepositoriesSlugs(ctx context.Context, organization string, slug string, data map[string]interface{}) (*PipelineRepositoriesSlugsResponse, error) {
	query := `query Pipelines($org: ID!, $first: Int!, $after: String, $search: String) {
  organization(slug: $org) {
    pipelines(first: $first, after: $after, order: RELEVANCE, search: $search) {
      pageInfo {
        hasNextPage
        endCursor
      }
      edges {
        node {
          slug
          repository {
            url
          }
          builds(first: 2) {
            edges {
              node {
                createdAt
              }
            }
          }
        }
      }
    }
  }
}`
	if data == nil {
		data = make(map[string]interface{})
	}
	req := &GraphQLRequest{Query: query, Variables: data}
	data["org"] = organization
	data["search"] = slug
	if _, ok := data["first"]; !ok {
		data["first"] = 100
	}
	if _, ok := data["after"]; !ok {
		data["after"] = nil
	}
	queryData, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var resp PipelineRepositoriesSlugsResponse
	if err := c.client.GraphQLRequest(ctx, string(queryData), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
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

func (o *OrganizationService) ListPipelines(ctx context.Context, data url.Values) ([]Pipeline, error) {
	path := "/organizations/" + o.org + "/pipelines"
	var val []Pipeline
	err := o.client.ListResource(ctx, path, data, &val)
	return val, err
}

type BuildService struct {
	client   *Client
	org      string
	pipeline string
	number   int64
}

func (p *PipelineService) Build(number int64) *BuildService {
	return &BuildService{
		client:   p.client,
		org:      p.org,
		pipeline: p.pipeline,
		number:   number,
	}
}

func (p *PipelineService) ListBuilds(ctx context.Context, query url.Values) (ListBuildResponse, error) {
	path := "/organizations/" + p.org + "/pipelines/" + p.pipeline + "/builds"
	var val ListBuildResponse
	err := p.client.ListResource(ctx, path, query, &val)
	return val, err
}

type JobService struct {
	client      *Client
	org         string
	pipeline    string
	buildNumber int64
	jobID       string
}

func (b *BuildService) Job(id string) *JobService {
	return &JobService{
		client:      b.client,
		org:         b.org,
		pipeline:    b.pipeline,
		buildNumber: b.number,
		jobID:       id,
	}
}

func (b *BuildService) Path() string {
	return fmt.Sprintf("/organizations/%s/pipelines/%s/builds/%d",
		b.org, b.pipeline, b.number)
}

func (b *BuildService) Annotations(ctx context.Context, query url.Values) (AnnotationResponse, error) {
	path := b.Path() + "/annotations"
	var val AnnotationResponse
	err := b.client.ListResource(ctx, path, query, &val)
	return val, err
}

func (j *JobService) Path() string {
	return fmt.Sprintf("/organizations/%s/pipelines/%s/builds/%d/jobs/%s",
		j.org, j.pipeline, j.buildNumber, j.jobID)
}

func (j *JobService) Log(ctx context.Context) (Log, error) {
	path := j.Path() + "/log"
	var val Log
	err := j.client.ListResource(ctx, path, nil, &val)
	return val, err
}

func (j *JobService) RawLog(ctx context.Context) ([]byte, error) {
	req, err := j.client.NewRequestWithContext(ctx, "GET", "/"+APIVersion+j.Path()+"/log", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/plain")
	resp, err := j.client.Client.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, restclient.DefaultErrorParser(resp)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (c *Client) Organization(org string) *OrganizationService {
	return &OrganizationService{client: c, org: org}
}

func isatty() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func (c *Client) BuildSummary(ctx context.Context, org string, build Build, numOutputLines int) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{'\n'}) // the end of the '=' line
	writer := tabwriter.NewWriter(&buf, 0, 0, 1, ' ', 0)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	/*
		build, err := client.Builds.Get(ctx, latestBuild.ID, "build.jobs", "job.config")
		if err == nil {
			stats, err := client.BuildSummary(ctx, build)
			if err == nil {
				fmt.Print(stats)
			} else {
				fmt.Printf("error fetching build summary: %v\n", err)
			}
		} else {
			fmt.Printf("error getting build: %v\n", err)
		}
	*/
	var failure []byte
	for i := range build.Jobs {
		duration := build.Jobs[i].FinishedAt.Time.Sub(build.Jobs[i].StartedAt)
		if duration > time.Minute {
			duration = duration.Round(time.Second)
		} else {
			duration = duration.Round(10 * time.Millisecond)
		}
		var durString string
		if build.Jobs[i].Failed() && isatty() {
			durString = fmt.Sprintf("\033[38;05;160m%-8s\033[0m", duration.String())
		} else {
			durString = duration.String()
		}
		if build.Jobs[i].Failed() && failure == nil {
			logs, err := c.Organization(org).Pipeline(build.Pipeline.Slug).Build(build.Number).Job(build.Jobs[i].ID).RawLog(ctx)
			if err == nil {
				// TODO: configure based on window?
				failure = FindBuildFailure(logs, numOutputLines)
			}
		}
		fmt.Fprintf(writer, "%s\t%s\n", build.Jobs[i].Name, durString)
	}
	writer.Flush()
	linelen := bytes.IndexByte(buf.Bytes()[1:], '\n')
	var buf2 bytes.Buffer
	buf2.WriteByte('\n')
	buf2.Write(bytes.Repeat([]byte{'='}, linelen))
	if len(failure) > 0 {
		fmt.Fprintf(&buf2, "\nLast %d lines of failed build output:\n\n", numOutputLines)
		buf2.Write(failure)
	}
	return append(buf.Bytes(), buf2.Bytes()...)
}

const Host = "https://api.buildkite.com"
const GraphQLHost = "https://graphql.buildkite.com"

func getHost() string {
	return Host
}
