package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"time"

	buildkite "github.com/kevinburke/buildkite/lib"
)

// exitTemporary is the exit status for a wait that could not reach a verdict
// for a reason that is not a red build: the token was throttled, or no build
// was ever created for the commit. Callers -- merge-on-green above all --
// need to tell those apart from a genuine failure, because the first is worth
// retrying and the second means the change is broken. Before this existed
// both left the same status 1, and a throttled token read as a failed build.
//
// 75 is EX_TEMPFAIL from sysexits(3).
const exitTemporary = 75

// temporaryError marks an error as an infrastructure problem rather than a
// verdict about the code under test.
type temporaryError struct {
	err error
}

func (t *temporaryError) Error() string { return t.err.Error() }
func (t *temporaryError) Unwrap() error { return t.err }

func temporary(format string, args ...any) error {
	return &temporaryError{err: fmt.Errorf(format, args...)}
}

// isTemporary reports whether err describes CI infrastructure failing to
// answer, as opposed to answering that the build failed.
func isTemporary(err error) bool {
	if _, ok := errors.AsType[*temporaryError](err); ok {
		return true
	}
	// A refusal that outlived the transport's own retries is still a refusal,
	// not a build result.
	if berr, ok := errors.AsType[*buildkite.Error](err); ok {
		return berr.StatusCode == http.StatusTooManyRequests ||
			(berr.StatusCode >= 500 && berr.StatusCode <= 599)
	}
	return false
}

// Polling constants for doWait. These are deliberately not flags: the right
// value is a property of Buildkite's rate limit and of how fast the git
// post-receive hook creates a build, not something a caller should be
// guessing at the command line.
const (
	// basePollInterval is how often to ask about a build we are watching.
	basePollInterval = 3 * time.Second
	// maxPollInterval bounds the stretch applied under a scarce budget. The
	// rate limit window is a minute, so waiting longer than this only makes
	// the wait less responsive without saving further requests.
	maxPollInterval = 30 * time.Second
	// buildCreateTimeout is how long to keep asking for a build that does not
	// exist yet. The build is created by a fire-and-forget POST from the git
	// post-receive hook, so it lands a moment after the push -- and if that
	// POST was itself throttled, it never lands at all. Waiting distinguishes
	// the two; before this, the first miss was fatal within a second.
	buildCreateTimeout = 3 * time.Minute
)

// pollInterval stretches the poll cadence as the shared rate limit window is
// used up.
//
// The budget belongs to the API token, not to this process: other `buildkite
// wait` runs, other tools, and the post-receive hook's build POST all draw on
// the same 200 requests a minute. So the reserve below is not politeness. A
// waiter that spends the last of the window is not slowing itself down, it is
// refusing the hook's POST -- and a refused POST means no build is created at
// all, which is the failure this whole path exists to survive.
func pollInterval(base time.Duration, budget *buildkite.Budget, now time.Time) time.Duration {
	snap, ok := budget.Snapshot()
	if !ok || snap.Limit <= 0 {
		return base
	}
	// While the window is comfortable, do not slow down at all: pacing early
	// would make every wait less responsive to buy headroom nobody needs.
	if snap.Remaining > snap.Limit/4 {
		return base
	}
	reserve := snap.Limit / 10
	left := max(snap.Reset.Sub(now), 0)
	spendable := snap.Remaining - reserve
	if spendable <= 0 {
		// Nothing left that is ours to spend. Sit out the rest of the window
		// rather than spend it collecting refusals.
		return clampPoll(base, left+time.Second)
	}
	return clampPoll(base, left/time.Duration(spendable))
}

func clampPoll(base, d time.Duration) time.Duration {
	if d < base {
		return base
	}
	if d > maxPollInterval {
		return maxPollInterval
	}
	return d
}

// sleepUntilNextPoll waits before the next request, stretching the interval
// when the shared rate limit window is running low. It reports ctx.Err() so a
// cancelled wait stops promptly rather than at the end of a stretched sleep.
func sleepUntilNextPoll(ctx context.Context, client *buildkite.Client) error {
	d := pollInterval(basePollInterval, client.Budget(), time.Now())
	if d > basePollInterval {
		slog.Debug("Slowing poll to stay inside the shared rate limit window", "interval", d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// pipelineSlugConfigKey names the git config entry holding the Buildkite
// pipeline for this checkout. Discovery costs a burst of API requests against
// a window shared with everything else using the token, and its answer does
// not change from one run to the next, so it is written down. `git config
// --unset buildkite.pipeline` clears it, and --pipeline overrides it.
const pipelineSlugConfigKey = "buildkite.pipeline"

func cachedPipelineSlug() (string, bool) {
	out, err := exec.Command("git", "config", "--get", pipelineSlugConfigKey).Output()
	if err != nil {
		return "", false
	}
	slug := strings.TrimSpace(string(out))
	return slug, slug != ""
}

func cachePipelineSlug(slug string) {
	if slug == "" {
		return
	}
	if err := exec.Command("git", "config", pipelineSlugConfigKey, slug).Run(); err != nil {
		// Not being able to write the cache costs a repeat of the search on
		// the next run, which is slow, not wrong.
		slog.Debug("Could not cache the pipeline slug", "slug", slug, "err", err)
	}
}
