package lib

import (
	"context"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Buildkite's REST rate limit is enforced per API token over a fixed
// one-minute window, and every consumer of a token shares it: each concurrent
// `buildkite wait`, the git post-receive hook that creates builds, and any
// ad-hoc script. Every response carries the current state of that window in
// its headers, which makes the headers the only cross-process coordination
// signal available -- no lock file, no shared state on disk.
//
// https://buildkite.com/docs/apis/rest-api#rate-limits
const (
	rateLimitLimitHeader     = "RateLimit-Limit"
	rateLimitRemainingHeader = "RateLimit-Remaining"
	rateLimitResetHeader     = "RateLimit-Reset"
	retryAfterHeader         = "Retry-After"
)

// BudgetSnapshot is the state of the rate limit window as of the most recent
// response.
type BudgetSnapshot struct {
	// Limit is the number of requests allowed in a window.
	Limit int
	// Remaining is how many of those were left when the response was served.
	Remaining int
	// Reset is when the current window rolls over.
	Reset time.Time
	// At is when the response carrying these values was received.
	At time.Time
}

// Budget records the rate limit headers from the most recent response. The
// zero value is ready to use; Snapshot reports ok=false until a response has
// actually carried the headers, so callers must not assume a budget exists.
type Budget struct {
	mu   sync.Mutex
	snap BudgetSnapshot
	seen bool
}

// Snapshot returns the most recently observed rate limit window. ok is false
// if no response has carried the headers yet, in which case the caller should
// behave as it did before there was a budget at all.
func (b *Budget) Snapshot() (BudgetSnapshot, bool) {
	if b == nil {
		return BudgetSnapshot{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.snap, b.seen
}

func (b *Budget) observe(resp *http.Response, now time.Time) {
	if b == nil || resp == nil {
		return
	}
	limit, limitOK := headerInt(resp.Header, rateLimitLimitHeader)
	remaining, remainingOK := headerInt(resp.Header, rateLimitRemainingHeader)
	if !limitOK || !remainingOK {
		return
	}
	snap := BudgetSnapshot{Limit: limit, Remaining: remaining, At: now}
	if reset, ok := headerInt(resp.Header, rateLimitResetHeader); ok {
		snap.Reset = now.Add(time.Duration(reset) * time.Second)
	} else {
		snap.Reset = now
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	// Responses can finish out of order, and an older one carries a larger
	// Remaining than the window really has. Taking the newest observation
	// keeps the budget honest under concurrency.
	if b.seen && snap.At.Before(b.snap.At) {
		return
	}
	b.snap = snap
	b.seen = true
}

func headerInt(h http.Header, name string) (int, bool) {
	raw := h.Get(name)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return n, true
}

// retryTransport retries requests that Buildkite refused without performing
// them. A 429 is the important case: it is not a failure of the operation, it
// is a refusal to start it, and without this the single refused request out of
// thousands surfaces to the caller as though the build itself had gone red.
type retryTransport struct {
	base   http.RoundTripper
	budget *Budget
	// maxAttempts counts the first try, so 1 disables retrying.
	maxAttempts int
	// now and sleep are swapped out in tests.
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

// maxRetryWait bounds how long a single refusal can stall a request. The rate
// limit window is a minute, so a correct Reset never exceeds this; the cap is
// here so a malformed or hostile header cannot park the process.
const maxRetryWait = 90 * time.Second

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	attempts := max(t.maxAttempts, 1)
	for attempt := 1; ; attempt++ {
		if attempt > 1 {
			// http.Request bodies are single-use. GetBody is populated by
			// http.NewRequest for the body types this package sends; a
			// request without one cannot be replayed, so it is not retried.
			if req.Body != nil {
				if req.GetBody == nil {
					return t.base.RoundTrip(req)
				}
				body, err := req.GetBody()
				if err != nil {
					return nil, err
				}
				req.Body = body
			}
		}
		resp, err := t.base.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		t.budget.observe(resp, t.now())
		if attempt >= attempts || !t.retryable(req, resp) {
			return resp, nil
		}
		wait := t.retryWait(resp, attempt)
		// The response is being discarded, so drain enough of it to let the
		// connection be reused rather than torn down and redialed.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if err := t.sleep(req.Context(), wait); err != nil {
			return nil, err
		}
	}
}

// retryable reports whether the request can be sent again safely.
//
// A 429 is always safe: the request was refused before it was performed, so
// replaying it cannot duplicate a side effect. A 5xx is not -- the server may
// well have performed the request before failing to say so -- so it is only
// retried for methods that carry no side effect. Retrying a POST /builds here
// would create a second build.
func (t *retryTransport) retryable(req *http.Request, resp *http.Response) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if resp.StatusCode >= 500 && resp.StatusCode <= 599 {
		return req.Method == http.MethodGet || req.Method == http.MethodHead
	}
	return false
}

func (t *retryTransport) retryWait(resp *http.Response, attempt int) time.Duration {
	// Buildkite states when the window rolls over; waiting any less than that
	// just spends another request on another refusal.
	if secs, ok := headerInt(resp.Header, retryAfterHeader); ok && secs >= 0 {
		return clampRetryWait(time.Duration(secs) * time.Second)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		if secs, ok := headerInt(resp.Header, rateLimitResetHeader); ok && secs >= 0 {
			return clampRetryWait(time.Duration(secs) * time.Second)
		}
	}
	// No guidance: back off exponentially, with jitter so that several
	// waiters refused in the same instant do not all return in the same one.
	backoff := time.Duration(1<<min(attempt-1, 4)) * time.Second
	return clampRetryWait(backoff + time.Duration(rand.N(500))*time.Millisecond)
}

func clampRetryWait(d time.Duration) time.Duration {
	// A zero wait means "the window rolls over now", which is a full second
	// away at worst given the header's one-second resolution.
	if d < time.Second {
		return time.Second
	}
	if d > maxRetryWait {
		return maxRetryWait
	}
	return d
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ObserveForTest records a response's rate limit headers against b. It exists
// so tests in other packages can build a Budget describing a given window
// without reaching into unexported state.
func ObserveForTest(b *Budget, resp *http.Response, now time.Time) {
	b.observe(resp, now)
}
