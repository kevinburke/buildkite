package lib

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		resp, err := client.GraphQL().PipelineRepositoriesSlugs(ctx, "twilio", "repo", nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Data.Organization.Pipelines.Edges) == 0 {
			t.Fatal("expected to find pipelines, but found none")
		}
	})
}

func TestBuildCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("expected PUT request, got %s", r.Method)
		}
		expectedPath := "/v2/organizations/test-org/pipelines/test-pipeline/builds/123/cancel"
		if r.URL.Path != expectedPath {
			t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
		}

		build := Build{
			Number: 123,
			State:  "canceled",
			Branch: "main",
			Commit: "abc123",
			WebURL: "https://buildkite.com/test-org/test-pipeline/builds/123",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(build)
	}))
	defer server.Close()

	client := NewClient("test-token")
	client.Client.Base = server.URL

	ctx := context.Background()
	build, err := client.Organization("test-org").Pipeline("test-pipeline").Build(123).Cancel(ctx)
	if err != nil {
		t.Fatalf("Cancel() returned error: %v", err)
	}
	if build.Number != 123 {
		t.Errorf("expected build number 123, got %d", build.Number)
	}
	if build.State != "canceled" {
		t.Errorf("expected state 'canceled', got %s", build.State)
	}
}

func TestBuildCancelError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Build is not in a cancelable state",
		})
	}))
	defer server.Close()

	client := NewClient("test-token")
	client.Client.Base = server.URL

	ctx := context.Background()
	_, err := client.Organization("test-org").Pipeline("test-pipeline").Build(123).Cancel(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	berr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if berr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected status code %d, got %d", http.StatusUnprocessableEntity, berr.StatusCode)
	}
}

func TestBuildRebuild(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("expected PUT request, got %s", r.Method)
		}
		expectedPath := "/v2/organizations/test-org/pipelines/test-pipeline/builds/123/rebuild"
		if r.URL.Path != expectedPath {
			t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
		}

		// Rebuild returns a new build with a new number
		build := Build{
			Number: 124,
			State:  "scheduled",
			Branch: "main",
			Commit: "abc123",
			WebURL: "https://buildkite.com/test-org/test-pipeline/builds/124",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(build)
	}))
	defer server.Close()

	client := NewClient("test-token")
	client.Client.Base = server.URL

	ctx := context.Background()
	build, err := client.Organization("test-org").Pipeline("test-pipeline").Build(123).Rebuild(ctx)
	if err != nil {
		t.Fatalf("Rebuild() returned error: %v", err)
	}
	if build.Number != 124 {
		t.Errorf("expected new build number 124, got %d", build.Number)
	}
	if build.State != "scheduled" {
		t.Errorf("expected state 'scheduled', got %s", build.State)
	}
}
