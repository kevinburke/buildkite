package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	buildkite "github.com/kevinburke/buildkite/lib"
)

// waitBuildSearch widens a missing-build lookup after a grace period. Discover
// candidates once, then recheck their builds at a slower cadence than the
// primary pipeline. Once a build has been seen, keep following its pipeline.
type waitBuildSearch struct {
	slug       string
	pinned     bool
	foundBuild bool
	nextSearch time.Time
	candidates []ScoredSlug
}

func (s *waitBuildSearch) latest(ctx context.Context, client *buildkite.Client, org string, remote *RemoteURL, branch, commit string, now time.Time) (buildkite.Build, error) {
	build, err := getLatestBuildForCommit(ctx, client, org, s.slug, remote, branch, commit)
	if err == nil {
		s.foundBuild = true
		return build, nil
	}
	if (err != errNoBuilds && err != errNoPipelineBuilds) || s.pinned || s.foundBuild || now.Before(s.nextSearch) {
		return buildkite.Build{}, err
	}
	s.nextSearch = now.Add(pipelineFallbackInterval)
	slog.Info("No build in selected pipeline; checking alternatives", "pipeline", s.slug, "commit", commit)
	if s.candidates == nil {
		candidates, err := findPipelineSlugs(ctx, client, org, remote)
		if err != nil {
			return buildkite.Build{}, temporary("searching alternative pipelines: %w", err)
		}
		s.candidates = candidates
	}
	// Discovery returns candidates in repository-score order. Only an actual
	// match for this SHA can replace the current pipeline, not build history.
	for _, candidate := range s.candidates {
		if candidate.Slug == s.slug {
			continue
		}
		build, candidateErr := getLatestBuildForCommit(ctx, client, org, candidate.Slug, remote, branch, commit)
		if candidateErr == errNoBuilds || candidateErr == errNoPipelineBuilds {
			continue
		}
		if berr, ok := errors.AsType[*buildkite.Error](candidateErr); ok && berr.StatusCode == http.StatusNotFound {
			continue // The pipeline may have been removed since discovery.
		}
		if candidateErr != nil {
			return buildkite.Build{}, candidateErr
		}
		s.slug = candidate.Slug
		s.foundBuild = true
		slog.Info("Found build in alternative pipeline", "pipeline", s.slug, "number", build.Number, "url", build.WebURL)
		return build, nil
	}
	return buildkite.Build{}, err
}
