package lib

import (
	"context"
	"testing"
)

func TestGraphQL(t *testing.T) {
	t.Skip("this hits the real API, TODO rework to use local server")
	ctx := context.Background()
	cfg, err := LoadConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	token, err := cfg.Token("kevinburke")
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(token)
	t.Run("Can", func(t *testing.T) {
		can, err := client.GraphQL().Can(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if can != true {
			t.Fatal("expected to be able to use GraphQL, but was not able to")
		}
	})
	t.Run("PipelineRepositoriesSlugs", func(t *testing.T) {
		resp, err := client.GraphQL().PipelineRepositoriesSlugs(ctx, "twilio", nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Data.Organization.Pipelines.Edges) == 0 {
			t.Fatal("expected to find pipelines, but found none")
		}
	})
}
