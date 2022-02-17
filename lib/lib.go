package lib

import (
	"context"
	"net/url"

	"github.com/kevinburke/rest/restclient"
)

type Client struct {
	*restclient.Client
}

type OrganizationService struct {
	client *restclient.Client
	org    string
}

type PipelineService struct {
	client   *restclient.Client
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

func (p *PipelineService) ListBuilds(ctx context.Context, query url.Values) (interface{}, error) {
	return nil, nil
}

func (c *Client) Organization(org string) *OrganizationService {
	return &OrganizationService{client: c.Client, org: org}
}

const Host = "https://api.buildkite.com"

func getHost() string {
	return Host
}

func GetToken() (string, error) {
	return "token", nil
}

func NewClient(token string) *Client {
	host := getHost()
	if host == "" {
		host = Host
	}
	rc := restclient.New("", "", host)
	return &Client{Client: rc}
}
