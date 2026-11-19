package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"

	buildkite "github.com/kevinburke/buildkite/lib"
)

func TestRebuildCreatesMissingBuild(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=main"},
		{"-c", "core.hooksPath=/dev/null", "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--quiet", "--allow-empty", "--message=Build missing commit"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	commit, err := gitTip("main")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name         string
		existing     bool
		empty        bool
		renamed      bool
		buildNumber  int64
		lookupStatus int
		createStatus int
		wantStatus   int
		wantMethod   string
		wantSuffix   string
	}{
		{name: "create for unbuilt commit", wantMethod: "POST"},
		{name: "create first pipeline build", empty: true, wantMethod: "POST"},
		{name: "create in discovered pipeline", renamed: true, wantMethod: "POST"},
		{name: "rebuild existing commit", existing: true, wantMethod: "PUT", wantSuffix: "/123/rebuild"},
		{name: "explicit build number", buildNumber: 123, wantMethod: "PUT", wantSuffix: "/123/rebuild"},
		{name: "lookup denied", lookupStatus: http.StatusForbidden, wantStatus: http.StatusForbidden},
		{name: "creation rejected", createStatus: http.StatusUnprocessableEntity, wantStatus: http.StatusUnprocessableEntity, wantMethod: "POST"},
		{name: "creation server error is not retried", createStatus: http.StatusInternalServerError, wantStatus: http.StatusInternalServerError, wantMethod: "POST"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pipeline := "repo"
			if tt.renamed {
				pipeline = "custom-pipeline"
			}
			buildsPath := "/v2/organizations/ci-org/pipelines/" + pipeline + "/builds"
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var response any
				switch {
				case r.Method != http.MethodGet && r.URL.Path != "/v1":
					writes.Add(1)
					if r.Method != tt.wantMethod || r.URL.Path != buildsPath+tt.wantSuffix {
						t.Errorf("unexpected write: %s %s", r.Method, r.URL)
						w.WriteHeader(http.StatusBadRequest)
						response = map[string]string{"message": "unexpected write"}
						break
					}
					if r.Method == http.MethodPost {
						// Buildkite's create-build endpoint expects a JSON body.
						var payload struct {
							Commit  string `json:"commit"`
							Branch  string `json:"branch"`
							Message string `json:"message"`
						}
						if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
							t.Errorf("decode create-build body: %v", err)
						}
						if payload.Commit != commit || payload.Branch != "main" || strings.TrimSpace(payload.Message) != "Build missing commit" {
							t.Errorf("create-build payload = %+v, want commit %s, branch main and commit message", payload, commit)
						}
						if got := r.Header.Get("Content-Type"); got != "application/json" {
							t.Errorf("Content-Type = %q, want application/json", got)
						}
					}
					if tt.createStatus != 0 {
						w.WriteHeader(tt.createStatus)
						response = map[string]string{"message": "creation failed"}
					} else {
						response = buildkite.Build{Number: 124, Commit: commit, Branch: "main", State: "scheduled", WebURL: "https://buildkite.com/ci-org/" + pipeline + "/builds/124"}
					}
				case r.URL.Path == "/v1":
					w.WriteHeader(http.StatusForbidden)
					response = map[string]string{"message": "GraphQL unavailable"}
				case r.URL.Path == "/v2/organizations/ci-org/pipelines":
					response = []buildkite.Pipeline{{Slug: pipeline, Repository: "https://github.com/org/repo.git"}}
				case r.URL.Path == buildsPath:
					if tt.buildNumber != 0 {
						t.Error("explicit build number must not look up the branch or commit")
					}
					if tt.lookupStatus != 0 {
						w.WriteHeader(tt.lookupStatus)
						response = map[string]string{"message": "lookup failed"}
						break
					}
					builds := []buildkite.Build{}
					if tt.existing {
						builds = append(builds, buildkite.Build{Number: 123, Commit: commit, Branch: "main"})
					} else if r.URL.Query().Get("commit") == "" && !tt.empty {
						builds = append(builds, buildkite.Build{Number: 122, Commit: strings.Repeat("b", 40), Branch: "main"})
					}
					response = builds
				default:
					if tt.renamed && r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/ci-org/pipelines/repo/builds" {
						w.WriteHeader(http.StatusNotFound)
						response = map[string]string{"message": "pipeline not found"}
					} else {
						t.Errorf("unexpected request: %s %s", r.Method, r.URL)
						w.WriteHeader(http.StatusNotFound)
						response = map[string]string{"message": "unexpected request"}
					}
				}
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			client := buildkite.NewClient("test-token")
			client.Client.Base = server.URL
			client.GraphQLClient.Base = server.URL
			err := doRebuild(t.Context(), client, buildkite.Organization{Name: "ci-org"}, &RemoteURL{Path: "org", RepoName: "repo"}, "main", tt.buildNumber)
			if tt.wantStatus != 0 {
				var apiErr *buildkite.Error
				if !errors.As(err, &apiErr) || apiErr.StatusCode != tt.wantStatus {
					t.Errorf("doRebuild error = %v, want HTTP %d", err, tt.wantStatus)
				}
			} else if err != nil {
				t.Errorf("doRebuild: %v", err)
			}
			var wantWrites int32
			if tt.wantMethod != "" {
				wantWrites = 1
			}
			if got := writes.Load(); got != wantWrites {
				t.Errorf("write requests = %d, want %d", got, wantWrites)
			}
		})
	}
}
