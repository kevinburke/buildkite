package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	buildkite "github.com/kevinburke/buildkite/lib"
)

// logsOptions configures doLogs.
type logsOptions struct {
	// BuildNumber selects a build directly; zero means the latest build for
	// the tip of the branch.
	BuildNumber int64
	// Pipeline pins the pipeline slug, skipping the search for one.
	Pipeline string
	// Dir is the directory to write logs into. Empty means a new temporary
	// directory.
	Dir string
	// FailedOnly skips jobs that did not fail.
	FailedOnly bool
	// Raw keeps the terminal escape sequences Buildkite stores in the log.
	Raw bool
}

// doLogs downloads the job logs for a build into a directory, lists the files
// it wrote on stderr, and prints the directory on stdout so it can be captured
// by a script: `cd "$(buildkite logs)"`.
func doLogs(ctx context.Context, client *buildkite.Client, org buildkite.Organization, remote *RemoteURL, branch string, opts logsOptions) error {
	slug, build, err := findBuild(ctx, client, org.Name, remote, branch, opts.BuildNumber, opts.Pipeline)
	if err != nil {
		return err
	}
	dir := opts.Dir
	createdDir := false
	if dir == "" {
		dir, err = os.MkdirTemp("", fmt.Sprintf("buildkite-%s-%d-", slug, build.Number))
		if err != nil {
			return err
		}
		createdDir = true
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	svc := client.Organization(org.Name).Pipeline(slug).Build(build.Number)
	fetch := func(ctx context.Context, jobID string) ([]byte, error) {
		return svc.Job(jobID).RawLog(ctx)
	}
	written, err := writeJobLogs(ctx, fetch, build, dir, opts, os.Stderr)
	if written > 0 {
		// Print the directory even if some jobs failed to download, so the
		// logs we did get are not lost; the error still sets the exit code.
		fmt.Println(dir)
	} else if createdDir {
		// Don't leave an empty temporary directory behind.
		os.Remove(dir)
	}
	return err
}

// findBuild returns the pipeline slug and the build to act on: build number
// buildNumber if it is set, otherwise the latest build for the tip of branch.
func findBuild(ctx context.Context, client *buildkite.Client, orgName string, remote *RemoteURL, branch string, buildNumber int64, pipeline string) (string, buildkite.Build, error) {
	slug := remote.RepoName
	pinned := pipeline != ""
	if pinned {
		slug = pipeline
	} else if cached, ok := cachedPipelineSlug(); ok {
		slog.Debug("Using cached pipeline slug", "slug", cached)
		slug = cached
	}

	if buildNumber > 0 {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		build, err := client.Organization(orgName).Pipeline(slug).Build(buildNumber).Get(ctx)
		if err != nil {
			return "", buildkite.Build{}, fmt.Errorf("getting build %d in pipeline %q: %w", buildNumber, slug, err)
		}
		return slug, build, nil
	}

	tip, err := gitTip(branch)
	if err != nil {
		return "", buildkite.Build{}, err
	}
	build, err := getLatestBuildForCommit(ctx, client, orgName, slug, branch, tip)
	if err == nil {
		return slug, build, nil
	}
	berr, isBuildkiteErr := err.(*buildkite.Error)
	notFound := (isBuildkiteErr && berr.StatusCode == 404) || err == errNoBuilds || err == errNoPipelineBuilds
	if pinned || !notFound {
		if notFound {
			return "", buildkite.Build{}, fmt.Errorf("no build found for commit %s on branch %s in pipeline %q", tip, branch, slug)
		}
		return "", buildkite.Build{}, err
	}
	candidates, err := findPipelineSlugs(ctx, client, orgName, slug)
	if err != nil {
		return "", buildkite.Build{}, err
	}
	slug, err = tryPipelineCandidates(ctx, client, orgName, candidates, branch, tip)
	if err != nil {
		return "", buildkite.Build{}, err
	}
	build, err = getLatestBuildForCommit(ctx, client, orgName, slug, branch, tip)
	if err == errNoBuilds || err == errNoPipelineBuilds {
		return "", buildkite.Build{}, fmt.Errorf("no build found for commit %s on branch %s in pipeline %q", tip, branch, slug)
	}
	if err != nil {
		return "", buildkite.Build{}, err
	}
	cachePipelineSlug(slug)
	return slug, build, nil
}

// rawLogFunc fetches the raw log for a job. Tests substitute the network call.
type rawLogFunc func(ctx context.Context, jobID string) ([]byte, error)

// writeJobLogs writes one file per job into dir, and a table of what it wrote
// to w. It returns the number of files written. A job that fails to download
// does not stop the others, but is reported in the returned error.
func writeJobLogs(ctx context.Context, fetch rawLogFunc, build buildkite.Build, dir string, opts logsOptions, w io.Writer) (int, error) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	var errs []error
	written := 0
	for i, job := range build.Jobs {
		// Waiter, block and trigger steps show up as jobs but have no log.
		if job.LogURL == "" {
			continue
		}
		name := job.Name
		if name == "" {
			name = job.Command
		}
		if opts.FailedOnly && !job.Failed() {
			continue
		}
		if job.StartedAt.IsZero() {
			fmt.Fprintf(tw, "%s\t%s\t(not started, skipped)\n", name, job.State)
			continue
		}
		jobCtx, cancel := context.WithTimeout(ctx, time.Minute)
		data, err := fetch(jobCtx, job.ID)
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("downloading log for job %q (%s): %w", name, job.ID, err))
			fmt.Fprintf(tw, "%s\t%s\t(download failed)\n", name, job.State)
			continue
		}
		if !opts.Raw {
			data = buildkite.CleanLog(data)
		}
		path := filepath.Join(dir, jobLogFilename(i, name))
		if err := os.WriteFile(path, data, 0o644); err != nil {
			errs = append(errs, err)
			continue
		}
		written++
		fmt.Fprintf(tw, "%s\t%s\t%s\n", name, job.State, path)
	}
	if err := tw.Flush(); err != nil {
		errs = append(errs, err)
	}
	if written == 0 && len(errs) == 0 {
		if opts.FailedOnly {
			return 0, fmt.Errorf("build %d has no failed jobs with logs", build.Number)
		}
		return 0, fmt.Errorf("build %d has no jobs with logs", build.Number)
	}
	return written, errors.Join(errs...)
}

// jobLogFilename returns a filename for the i'th job in a build. The index
// keeps the files in pipeline order and keeps parallel jobs, which share a
// name, from overwriting each other.
func jobLogFilename(i int, name string) string {
	var b strings.Builder
	lastDash := true // don't start with a dash
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_':
			b.WriteRune(r)
			lastDash = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
		if b.Len() >= 60 {
			break
		}
	}
	slug := strings.Trim(b.String(), "-.")
	if slug == "" {
		slug = "job"
	}
	return fmt.Sprintf("%02d-%s.log", i+1, slug)
}
