package main

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	buildkite "github.com/kevinburke/buildkite/lib"
)

func TestPollIntervalWithoutABudget(t *testing.T) {
	// Nothing has been observed yet, so behave exactly as before there was a
	// budget at all rather than guess at one.
	if got := pollInterval(basePollInterval, new(buildkite.Budget), time.Now()); got != basePollInterval {
		t.Errorf("got %v, want the base interval %v", got, basePollInterval)
	}
}

// budgetAt builds a Budget reporting the given window, as if a response had
// just carried those headers.
func budgetAt(t *testing.T, now time.Time, limit, remaining, resetSeconds int) *buildkite.Budget {
	t.Helper()
	b := new(buildkite.Budget)
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("RateLimit-Limit", fmt.Sprint(limit))
	resp.Header.Set("RateLimit-Remaining", fmt.Sprint(remaining))
	resp.Header.Set("RateLimit-Reset", fmt.Sprint(resetSeconds))
	buildkite.ObserveForTest(b, resp, now)
	return b
}

func TestPollIntervalDoesNotSlowAHealthyWindow(t *testing.T) {
	now := time.Now()
	// Two concurrent waits at one request every three seconds spend 40 of 200
	// a minute. Pacing that would cost responsiveness for nothing.
	b := budgetAt(t, now, 200, 160, 30)
	if got := pollInterval(basePollInterval, b, now); got != basePollInterval {
		t.Errorf("got %v, want %v", got, basePollInterval)
	}
}

func TestPollIntervalStretchesAsTheWindowRunsDown(t *testing.T) {
	now := time.Now()
	// 30 left with 30 seconds to go, minus the 20 held back for everything
	// else on this token: 30s / 10 spendable = 3s... so push it further down.
	b := budgetAt(t, now, 200, 25, 30)
	got := pollInterval(basePollInterval, b, now)
	if got <= basePollInterval {
		t.Errorf("got %v; a scarce window must stretch the poll past %v", got, basePollInterval)
	}
	if got > maxPollInterval {
		t.Errorf("got %v, which exceeds the %v cap", got, maxPollInterval)
	}
}

func TestPollIntervalHoldsBackAReserve(t *testing.T) {
	now := time.Now()
	// Everything left is reserve. Spending it would not slow us down, it
	// would refuse the post-receive hook's build POST -- and then there is no
	// build to wait for at all.
	b := budgetAt(t, now, 200, 15, 20)
	got := pollInterval(basePollInterval, b, now)
	if got < 20*time.Second {
		t.Errorf("got %v, want a wait covering the rest of the window", got)
	}
	if got > maxPollInterval {
		t.Errorf("got %v, which exceeds the %v cap", got, maxPollInterval)
	}
}

func TestPollIntervalWithAnExpiredWindow(t *testing.T) {
	now := time.Now()
	b := budgetAt(t, now.Add(-time.Minute), 200, 0, 5)
	if got := pollInterval(basePollInterval, b, now); got != basePollInterval {
		t.Errorf("got %v, want %v: a window that already rolled over is not a reason to wait", got, basePollInterval)
	}
}

func TestIsTemporary(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"throttled", &buildkite.Error{StatusCode: http.StatusTooManyRequests, Message: "You have exceeded your rest_user API rate limit"}, true},
		{"server error", &buildkite.Error{StatusCode: http.StatusBadGateway, Message: "bad gateway"}, true},
		{"not found", &buildkite.Error{StatusCode: http.StatusNotFound, Message: "not found"}, false},
		{"no build created", temporary("no build was created"), true},
		{"red build", buildFailedError("branch", buildkite.Build{}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTemporary(tt.err); got != tt.want {
				t.Errorf("isTemporary(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestARedBuildIsNotTemporary(t *testing.T) {
	// The whole point of the distinction: a caller retries a throttle and
	// stops on a failure, and before this both left the same exit status.
	if isTemporary(buildFailedError("my-branch", buildkite.Build{})) {
		t.Fatal("a failed build must not be reported as a temporary error, or callers will retry a broken change forever")
	}
}
