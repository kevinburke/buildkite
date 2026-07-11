package lib

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kevinburke/go-types"
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

func TestListBuildsWithSlashInBranch(t *testing.T) {
	tests := []struct {
		name   string
		branch string
	}{
		{"simple slash", "feature/foo"},
		{"nested slashes", "feature/team/ticket-123"},
		{"dependabot style", "dependabot/go_modules/golang.org/x/net-0.25.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotBranch string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotBranch = r.URL.Query().Get("branch")
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]Build{
					{Number: 1, Branch: tt.branch, Commit: "abc123", State: "passed"},
				})
			}))
			defer server.Close()

			client := NewClient("test-token")
			client.Client.Base = server.URL

			params := url.Values{"branch": []string{tt.branch}}
			ctx := context.Background()
			builds, err := client.Organization("org").Pipeline("repo").ListBuilds(ctx, params)
			if err != nil {
				t.Fatalf("ListBuilds returned error: %v", err)
			}
			if gotBranch != tt.branch {
				t.Errorf("server received branch %q, want %q", gotBranch, tt.branch)
			}
			if len(builds) != 1 {
				t.Fatalf("got %d builds, want 1", len(builds))
			}
			if builds[0].Branch != tt.branch {
				t.Errorf("build branch = %q, want %q", builds[0].Branch, tt.branch)
			}
		})
	}
}

func TestBuildSummaryShowsExitStatus(t *testing.T) {
	startedAt := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(5 * time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/organizations/test-org/pipelines/test-pipeline/builds/123/jobs/job-1/log" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("failure output\n"))
	}))
	defer server.Close()

	client := NewClient("test-token")
	client.Client.Base = server.URL

	out := client.BuildSummary(context.Background(), "test-org", Build{
		Number: 123,
		Pipeline: Pipeline{
			Slug: "test-pipeline",
		},
		Jobs: []Job{
			{
				ID:         "job-1",
				Name:       "test",
				State:      "failed",
				ExitStatus: intPtr(2),
				StartedAt:  startedAt,
				FinishedAt: nullTime(finishedAt),
			},
		},
	}, 10)

	got := string(out)
	if !strings.Contains(got, "test 5s exit status 2") {
		t.Fatalf("BuildSummary() did not include exit status:\n%s", got)
	}
}

func TestBuildSummaryShowsAgentLostExitStatus(t *testing.T) {
	startedAt := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(5 * time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("agent disappeared\n"))
	}))
	defer server.Close()

	client := NewClient("test-token")
	client.Client.Base = server.URL

	out := client.BuildSummary(context.Background(), "test-org", Build{
		Number: 123,
		Pipeline: Pipeline{
			Slug: "test-pipeline",
		},
		Jobs: []Job{
			{
				ID:         "job-1",
				Name:       "integration",
				State:      "failed",
				ExitStatus: intPtr(-1),
				StartedAt:  startedAt,
				FinishedAt: nullTime(finishedAt),
			},
		},
	}, 10)

	got := string(out)
	if !strings.Contains(got, "integration 5s exit status -1 (agent lost)") {
		t.Fatalf("BuildSummary() did not include agent lost exit status:\n%s", got)
	}
}

func nullTime(t time.Time) types.NullTime {
	return types.NullTime{Time: t, Valid: true}
}
