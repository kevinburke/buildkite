package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	buildkite "github.com/kevinburke/buildkite/lib"
)

func TestWaitBuildSearch(t *testing.T) {
	type step struct {
		after      time.Duration
		wantBuild  int64
		wantStatus int
	}
	tests := []struct {
		name              string
		pinned            bool
		primaryAt         time.Duration
		primaryStatus     int
		primaryDisappears bool
		forkAt            time.Duration
		forkStatus        int
		steps             []step
		wantDiscoveries   int32
	}{
		{
			name: "search forks after thirty seconds", primaryAt: 45 * time.Second,
			steps:           []step{{after: 0}, {after: 30*time.Second - time.Nanosecond}, {after: 30 * time.Second, wantBuild: 9}, {after: time.Minute, wantBuild: 9}},
			wantDiscoveries: 1,
		},
		{
			name: "late fork build", primaryAt: time.Hour, forkAt: time.Minute,
			steps:           []step{{after: 30 * time.Second}, {after: 33 * time.Second}, {after: 59 * time.Second}, {after: time.Minute, wantBuild: 9}},
			wantDiscoveries: 1,
		},
		{
			name: "explicit pipeline stays pinned", pinned: true, primaryAt: time.Hour,
			steps: []step{{after: 0}, {after: 30 * time.Second}, {after: time.Minute}},
		},
		{
			name: "primary build appears during grace period", primaryAt: 27 * time.Second,
			steps: []step{{after: 0}, {after: 27 * time.Second, wantBuild: 1}, {after: time.Minute, wantBuild: 1}},
		},
		{
			name: "do not switch after seeing a build", primaryDisappears: true,
			steps: []step{{after: 0, wantBuild: 1}, {after: 30 * time.Second}, {after: time.Minute}},
		},
		{
			name: "missing alternatives do not replace primary", primaryAt: time.Hour, forkAt: time.Hour,
			steps:           []step{{after: 30 * time.Second}, {after: time.Minute}},
			wantDiscoveries: 1,
		},
		{
			name: "primary API failure does not trigger fallback", primaryStatus: http.StatusForbidden,
			steps: []step{{after: 30 * time.Second, wantStatus: http.StatusForbidden}},
		},
		{
			name: "API failure is not a missing build", primaryAt: time.Hour, forkStatus: http.StatusForbidden,
			steps:           []step{{after: 30 * time.Second, wantStatus: http.StatusForbidden}},
			wantDiscoveries: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commit := strings.Repeat("a", 40)
			var elapsed atomic.Int64
			var discoveries, forkRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var response any
				switch r.URL.Path {
				case "/v1":
					w.WriteHeader(http.StatusForbidden)
					response = map[string]string{"message": "GraphQL unavailable"}
				case "/v2/organizations/org/pipelines":
					discoveries.Add(1)
					response = []buildkite.Pipeline{
						{Slug: "other-fork", Repository: "https://github.com/fork/repo.git"},
						{Slug: "repo", Repository: "https://github.com/org/repo.git"},
						{Slug: "preferred-fork", Repository: "https://github.com/org-extra/repo.git"},
					}
				case "/v2/organizations/org/pipelines/repo/builds",
					"/v2/organizations/org/pipelines/preferred-fork/builds",
					"/v2/organizations/org/pipelines/other-fork/builds":
					isPrimary := strings.Contains(r.URL.Path, "/repo/builds")
					if !isPrimary {
						forkRequests.Add(1)
					}
					queryCommit := r.URL.Query().Get("commit")
					if queryCommit != "" && queryCommit != commit {
						t.Errorf("queried commit %q, want %q", queryCommit, commit)
					}
					age := time.Duration(elapsed.Load())
					number := int64(9)
					available := age >= tt.forkAt
					if isPrimary {
						if tt.primaryStatus != 0 {
							w.WriteHeader(tt.primaryStatus)
							response = map[string]string{"message": "access denied"}
							break
						}
						number = 1
						available = age >= tt.primaryAt && !(tt.primaryDisappears && age >= 10*time.Second)
					} else if tt.forkStatus != 0 {
						w.WriteHeader(tt.forkStatus)
						response = map[string]string{"message": "access denied"}
						break
					} else if strings.Contains(r.URL.Path, "/other-fork/") {
						number = 10
					}
					builds := []buildkite.Build{}
					if available {
						builds = append(builds, buildkite.Build{Number: number, Commit: commit, Branch: "branch", State: "running"})
					} else if queryCommit == "" {
						// The pipeline exists and has history, just not our commit.
						builds = append(builds, buildkite.Build{Number: 2, Commit: strings.Repeat("b", 40)})
					}
					response = builds
				default:
					t.Errorf("unexpected request: %s", r.URL)
					w.WriteHeader(http.StatusNotFound)
					response = map[string]string{"message": "unexpected request"}
				}
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			client := buildkite.NewClient("test-token")
			client.Client.Base = server.URL
			client.GraphQLClient.Base = server.URL
			start := time.Now()
			search := waitBuildSearch{slug: "repo", pinned: tt.pinned, nextSearch: start.Add(30 * time.Second)}
			for _, step := range tt.steps {
				elapsed.Store(int64(step.after))
				beforeForkRequests := forkRequests.Load()
				beforeNextSearch := search.nextSearch
				build, err := search.latest(t.Context(), client, "org", &RemoteURL{Path: "org", RepoName: "repo"}, "branch", commit, start.Add(step.after))
				var apiErr *buildkite.Error
				switch {
				case step.wantStatus != 0:
					if !errors.As(err, &apiErr) || apiErr.StatusCode != step.wantStatus {
						t.Fatalf("at %s: error = %v, want HTTP %d", step.after, err, step.wantStatus)
					}
				case step.wantBuild == 0:
					if !errors.Is(err, errNoBuilds) {
						t.Fatalf("at %s: error = %v, want no build", step.after, err)
					}
				case err != nil:
					t.Fatalf("at %s: %v", step.after, err)
				}
				if build.Number != step.wantBuild {
					t.Fatalf("at %s: build = %d, want %d", step.after, build.Number, step.wantBuild)
				}
				wantSlug := "repo"
				if step.wantBuild == 9 {
					wantSlug = "preferred-fork"
				}
				if search.slug != wantSlug {
					t.Errorf("at %s: pipeline = %q, want %q", step.after, search.slug, wantSlug)
				}
				if start.Add(step.after).Before(beforeNextSearch) && forkRequests.Load() != beforeForkRequests {
					t.Errorf("at %s: queried forks before the next search was due", step.after)
				}
			}
			if got := discoveries.Load(); got != tt.wantDiscoveries {
				t.Errorf("discovered pipelines %d times, want %d", got, tt.wantDiscoveries)
			}
		})
	}
}
