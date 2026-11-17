package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	buildkite "github.com/kevinburke/buildkite/lib"
)

func TestGetLatestBuildForCommit(t *testing.T) {
	commit := strings.Repeat("a", 40)
	branch := "feature/fix+builds"
	// Newer builds can crowd the wanted commit out of both the three-build
	// branch lookup and the thirty-build pipeline fallback. Some may be for
	// commits replaced by a force push whose builds were created later.
	var newer []buildkite.Build
	for i := 100; i > 50; i-- {
		newer = append(newer, buildkite.Build{
			Number: int64(i), Branch: branch, Commit: fmt.Sprintf("%040x", i), State: "running",
		})
	}
	newest := buildkite.Build{Number: 50, Branch: branch, Commit: commit, State: "running"}
	older := buildkite.Build{Number: 49, Branch: branch, Commit: commit, State: "canceled"}
	fork := buildkite.Build{Number: 50, Branch: "fork:" + branch, Commit: commit, State: "running"}
	otherBranch := buildkite.Build{Number: 101, Branch: "main", Commit: commit, State: "passed"}
	upstream := newest
	upstream.Pipeline.Repository = "git@github.com:org/repo.git"
	forkPR := upstream
	forkPR.Number = 101
	forkPR.State = "passed"
	forkPR.PullRequest = &buildkite.PullRequest{Repository: "https://github.com/fork/repo.git"}
	upstreamPR := upstream
	upstreamPR.PullRequest = &buildkite.PullRequest{Repository: "https://github.com/org/repo.git"}
	upstreamOtherBranch := upstream
	upstreamOtherBranch.Branch = "main"
	prefixedFork := upstream
	prefixedFork.Number = 101
	prefixedFork.Branch = "fork:" + branch
	relatedFork := forkPR
	relatedFork.Number = 50
	relatedFork.PullRequest = &buildkite.PullRequest{Repository: "https://github.com/org-contrib/repo.git"}
	failedRebuild := forkPR
	failedRebuild.Number = 102
	failedRebuild.State = "failed"
	var manyForks []buildkite.Build
	for i := 250; i > 50; i-- {
		build := forkPR
		build.Number = int64(i)
		manyForks = append(manyForks, build)
	}

	tests := []struct {
		name         string
		branch       string
		builds       []buildkite.Build
		want         buildkite.Build
		wantErr      error
		wantRequests int
	}{
		{
			name: "forks use repository similarity too", branch: branch,
			builds: []buildkite.Build{forkPR, relatedFork}, want: relatedFork, wantRequests: 1,
		},
		{
			name: "fork fallback keeps newest regardless of state", branch: branch,
			builds: []buildkite.Build{failedRebuild, forkPR}, want: failedRebuild, wantRequests: 1,
		},
		{
			name: "fork prefix without source metadata", branch: branch,
			builds: []buildkite.Build{prefixedFork, upstreamOtherBranch}, want: upstreamOtherBranch, wantRequests: 1,
		},
		{
			name: "upstream outranks newer fork on same branch", branch: branch,
			builds: []buildkite.Build{forkPR, upstream}, want: upstream, wantRequests: 1,
		},
		{
			name: "upstream pull request outranks newer fork", branch: branch,
			builds: []buildkite.Build{forkPR, upstreamPR}, want: upstreamPR, wantRequests: 1,
		},
		{
			name: "repository score outranks branch match", branch: branch,
			builds: []buildkite.Build{forkPR, upstreamOtherBranch}, want: upstreamOtherBranch, wantRequests: 1,
		},
		{
			name: "upstream on later page", branch: branch,
			builds: append(append([]buildkite.Build{}, manyForks...), upstream),
			want:   upstream, wantRequests: 3,
		},
		{
			name: "busy branch", branch: branch,
			builds: append(append([]buildkite.Build{}, newer...), newest, older),
			want:   newest, wantRequests: 1,
		},
		{
			name: "fork on busy pipeline", branch: branch,
			builds: append(append([]buildkite.Build{}, newer...), fork),
			want:   fork, wantRequests: 1,
		},
		{
			name: "prefer matching branch", branch: branch,
			builds: append(append([]buildkite.Build{otherBranch}, newer...), newest),
			want:   newest, wantRequests: 1,
		},
		{
			name: "latest rebuild", branch: branch,
			builds: []buildkite.Build{newest, older}, want: newest, wantRequests: 1,
		},
		{
			name:   "no branch filter",
			builds: append(append([]buildkite.Build{}, newer...), fork),
			want:   fork, wantRequests: 1,
		},
		{
			name: "commit not built", branch: branch,
			builds: newer, wantErr: errNoBuilds, wantRequests: 2,
		},
		{
			name: "pipeline empty", branch: branch,
			wantErr: errNoPipelineBuilds, wantRequests: 2,
		},
		{
			name:   "commit not built without branch",
			builds: newer, wantErr: errNoBuilds, wantRequests: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet || r.URL.Path != "/v2/organizations/ci-org/pipelines/ci-pipeline/builds" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				query := r.URL.Query()
				perPage, err := strconv.Atoi(query.Get("per_page"))
				if err != nil || perPage < 1 {
					t.Errorf("invalid per_page: %q", query.Get("per_page"))
					http.Error(w, "invalid per_page", http.StatusBadRequest)
					return
				}
				// Model the API: filter first, then paginate newest-first builds.
				builds := []buildkite.Build{}
				for _, build := range tt.builds {
					if branch := query.Get("branch"); branch != "" && build.Branch != branch {
						continue
					}
					if commit := query.Get("commit"); commit != "" && build.Commit != commit {
						continue
					}
					builds = append(builds, build)
				}
				page := 1
				if query.Has("page") {
					page, err = strconv.Atoi(query.Get("page"))
					if err != nil || page < 1 {
						t.Errorf("invalid page: %q", query.Get("page"))
						http.Error(w, "invalid page", http.StatusBadRequest)
						return
					}
				}
				start := min((page-1)*perPage, len(builds))
				builds = builds[start:min(start+perPage, len(builds))]
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(builds); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			client := buildkite.NewClient("test-token")
			client.Client.Base = server.URL

			// API organization and pipeline slugs need not match the git remote.
			got, err := getLatestBuildForCommit(t.Context(), client, "ci-org", "ci-pipeline", &RemoteURL{Path: "org", RepoName: "repo"}, tt.branch, commit)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("getLatestBuildForCommit() error = %v, want %v", err, tt.wantErr)
			}
			if got.Number != tt.want.Number || got.Commit != tt.want.Commit || got.State != tt.want.State {
				t.Errorf("got build %d (%s, %s), want %d (%s, %s)",
					got.Number, got.Commit, got.State, tt.want.Number, tt.want.Commit, tt.want.State)
			}
			if requests != tt.wantRequests {
				t.Errorf("made %d requests, want %d", requests, tt.wantRequests)
			}
		})
	}
}

func TestGetLatestBuildForCommitAPIError(t *testing.T) {
	tests := []struct {
		name     string
		failAt   int
		fullPage bool
	}{
		{name: "first page", failAt: 1},
		{name: "pipeline history probe", failAt: 2},
		{name: "later page after a match", failAt: 2, fullPage: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			commit := strings.Repeat("a", 40)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.Header().Set("Content-Type", "application/json")
				if requests == tt.failAt {
					w.WriteHeader(http.StatusForbidden)
					fmt.Fprint(w, `{"message":"access denied"}`)
					return
				}
				if tt.fullPage {
					builds := make([]buildkite.Build, 100)
					for i := range builds {
						builds[i] = buildkite.Build{Number: int64(100 - i), Commit: commit, Branch: "fork:branch"}
					}
					if err := json.NewEncoder(w).Encode(builds); err != nil {
						t.Error(err)
					}
					return
				}
				fmt.Fprint(w, `[]`)
			}))
			defer server.Close()
			client := buildkite.NewClient("test-token")
			client.Client.Base = server.URL

			_, err := getLatestBuildForCommit(t.Context(), client, "org", "repo", &RemoteURL{Path: "org", RepoName: "repo"}, "branch", commit)
			var apiErr *buildkite.Error
			if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden {
				t.Fatalf("got error %v, want HTTP 403", err)
			}
			if requests != tt.failAt {
				t.Errorf("made %d requests, want %d", requests, tt.failAt)
			}
		})
	}
}

func TestFindPipelineSlugsUsesGitRepository(t *testing.T) {
	for _, source := range []string{"REST", "REST paginated", "REST paginated error", "GraphQL"} {
		t.Run(source, func(t *testing.T) {
			pipelines := []buildkite.Pipeline{
				{Slug: "fork", Repository: "https://github.com/ci-org/repo.git"},
				{Slug: "official", Repository: "git@github.com:git-org/repo.git"},
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v2/organizations/ci-org/pipelines":
					if source == "GraphQL" {
						w.WriteHeader(http.StatusForbidden)
						fmt.Fprint(w, `{"message":"use GraphQL"}`)
						return
					}
					page := pipelines
					if strings.HasPrefix(source, "REST paginated") {
						switch r.URL.Query().Get("page") {
						case "1":
							page = make([]buildkite.Pipeline, 100)
							page[0] = pipelines[1] // Exact match, but the fork is on page two.
						case "2":
							if source == "REST paginated error" {
								w.WriteHeader(http.StatusForbidden)
								fmt.Fprint(w, `{"message":"access denied"}`)
								return
							}
							page = pipelines[:1]
						default:
							t.Errorf("unexpected pipeline page: %s", r.URL)
						}
					}
					if err := json.NewEncoder(w).Encode(page); err != nil {
						t.Error(err)
					}
				case "/v1":
					if source != "GraphQL" {
						w.WriteHeader(http.StatusForbidden)
						fmt.Fprint(w, `{"message":"use REST"}`)
						return
					}
					var query buildkite.GraphQLRequest
					if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
						t.Error(err)
						return
					}
					if strings.Contains(query.Query, "__typename") {
						fmt.Fprint(w, `{"data":{"__typename":"Query"}}`)
						return
					}
					if query.Variables["org"] != "ci-org" || query.Variables["search"] != "repo" {
						t.Errorf("unexpected GraphQL variables: %v", query.Variables)
					}
					var resp buildkite.PipelineRepositoriesSlugsResponse
					for _, p := range pipelines {
						resp.Data.Organization.Pipelines.Edges = append(resp.Data.Organization.Pipelines.Edges, buildkite.PipelineEdge{
							Node: buildkite.PipelineNode{Slug: p.Slug, Repository: buildkite.Repository{URL: p.Repository}},
						})
					}
					if err := json.NewEncoder(w).Encode(resp); err != nil {
						t.Error(err)
					}
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					http.Error(w, "unexpected request", http.StatusBadRequest)
				}
			}))
			defer server.Close()
			client := buildkite.NewClient("test-token")
			client.Client.Base = server.URL
			client.GraphQLClient.Base = server.URL
			candidates, err := findPipelineSlugs(t.Context(), client, "ci-org", &RemoteURL{Path: "git-org", RepoName: "repo"})
			if source == "REST paginated error" {
				if err == nil || len(candidates) != 0 {
					t.Fatalf("got candidates %v, error %v; want pagination failure without partial candidates", candidates, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(candidates) != 2 || candidates[0].Slug != "official" || candidates[0].Score != 1000 || candidates[1].Slug != "fork" || candidates[1].Score >= 1000 {
				t.Errorf("candidates = %v, want official repository before fork", candidates)
			}
		})
	}
}
