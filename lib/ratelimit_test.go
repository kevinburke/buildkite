package lib

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func newTestTransport(base http.RoundTripper, budget *Budget, slept *[]time.Duration) *retryTransport {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	return &retryTransport{
		base:        base,
		budget:      budget,
		maxAttempts: 3,
		now:         func() time.Time { return now },
		sleep: func(_ context.Context, d time.Duration) error {
			*slept = append(*slept, d)
			return nil
		},
	}
}

func TestBudgetRecordsRateLimitHeaders(t *testing.T) {
	budget := new(Budget)
	if _, ok := budget.Snapshot(); ok {
		t.Fatal("a budget that has seen no response must report ok=false, so that callers do not pace against a window they invented")
	}
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set(rateLimitLimitHeader, "200")
	resp.Header.Set(rateLimitRemainingHeader, "37")
	resp.Header.Set(rateLimitResetHeader, "12")
	budget.observe(resp, now)

	snap, ok := budget.Snapshot()
	if !ok {
		t.Fatal("expected the budget to be populated")
	}
	if snap.Limit != 200 || snap.Remaining != 37 {
		t.Errorf("got limit=%d remaining=%d, want 200/37", snap.Limit, snap.Remaining)
	}
	if want := now.Add(12 * time.Second); !snap.Reset.Equal(want) {
		t.Errorf("got reset %v, want %v", snap.Reset, want)
	}
}

func TestBudgetIgnoresAnOlderResponse(t *testing.T) {
	// Responses finish out of order, and a stale one claims more budget than
	// the window actually has left.
	budget := new(Budget)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	fresh := &http.Response{Header: http.Header{}}
	fresh.Header.Set(rateLimitLimitHeader, "200")
	fresh.Header.Set(rateLimitRemainingHeader, "5")
	budget.observe(fresh, now)

	stale := &http.Response{Header: http.Header{}}
	stale.Header.Set(rateLimitLimitHeader, "200")
	stale.Header.Set(rateLimitRemainingHeader, "180")
	budget.observe(stale, now.Add(-time.Second))

	snap, _ := budget.Snapshot()
	if snap.Remaining != 5 {
		t.Errorf("got remaining %d, want the newer observation 5", snap.Remaining)
	}
}

func TestBudgetIgnoresAResponseWithoutHeaders(t *testing.T) {
	budget := new(Budget)
	budget.observe(&http.Response{Header: http.Header{}}, time.Now())
	if _, ok := budget.Snapshot(); ok {
		t.Fatal("a response with no rate limit headers must leave the budget unset")
	}
}

func TestRetriesAThrottledRequest(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		w.Header().Set(rateLimitLimitHeader, "200")
		w.Header().Set(rateLimitRemainingHeader, "0")
		w.Header().Set(rateLimitResetHeader, "9")
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"message":"You have exceeded your rest_user API rate limit"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	var slept []time.Duration
	tr := newTestTransport(srv.Client().Transport, new(Budget), &slept)
	req, err := http.NewRequest("GET", srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want 200: a 429 is a refusal to perform the request, not a result", resp.StatusCode)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("made %d requests, want 2", got)
	}
	if len(slept) != 1 || slept[0] != 9*time.Second {
		t.Errorf("slept %v, want one wait of 9s taken from the reset header", slept)
	}
}

func TestPrefersRetryAfterOverReset(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}}
	resp.Header.Set(retryAfterHeader, "4")
	resp.Header.Set(rateLimitResetHeader, "40")
	tr := &retryTransport{}
	if got := tr.retryWait(resp, 1); got != 4*time.Second {
		t.Errorf("got %v, want 4s", got)
	}
}

func TestRetryWaitWithoutGuidanceBacksOff(t *testing.T) {
	tr := &retryTransport{}
	resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}}
	first := tr.retryWait(resp, 1)
	third := tr.retryWait(resp, 3)
	if third <= first {
		t.Errorf("got %v then %v; later attempts must wait longer", first, third)
	}
	if third > maxRetryWait {
		t.Errorf("got %v, which exceeds the %v cap", third, maxRetryWait)
	}
}

func TestGivesUpAfterMaxAttempts(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set(retryAfterHeader, "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	var slept []time.Duration
	tr := newTestTransport(srv.Client().Transport, new(Budget), &slept)
	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("got %d, want the refusal surfaced once retries are spent", resp.StatusCode)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("made %d requests, want maxAttempts=3", got)
	}
}

func TestDoesNotRetryAServerErrorOnAWrite(t *testing.T) {
	// A 5xx may mean the server did the work and failed to say so. Replaying
	// POST /builds would create a second build.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	for _, tt := range []struct {
		method string
		want   int32
	}{
		{"GET", 3},
		{"POST", 1},
		{"PUT", 1},
	} {
		t.Run(tt.method, func(t *testing.T) {
			calls.Store(0)
			var slept []time.Duration
			tr := newTestTransport(srv.Client().Transport, new(Budget), &slept)
			req, _ := http.NewRequest(tt.method, srv.URL, nil)
			resp, err := tr.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if got := calls.Load(); got != tt.want {
				t.Errorf("%s made %d requests, want %d", tt.method, got, tt.want)
			}
		})
	}
}

func TestRetriesAThrottledWrite(t *testing.T) {
	// Unlike a 5xx, a 429 is refused before anything happens, so replaying it
	// cannot duplicate a side effect.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set(retryAfterHeader, "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	var slept []time.Duration
	tr := newTestTransport(srv.Client().Transport, new(Budget), &slept)
	req, err := http.NewRequest("POST", srv.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("got %d, want 201", resp.StatusCode)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("made %d requests, want 2", got)
	}
}

func TestClientRecordsTheBudgetItWasServed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(rateLimitLimitHeader, "200")
		w.Header().Set(rateLimitRemainingHeader, strconv.Itoa(41))
		w.Header().Set(rateLimitResetHeader, "30")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client := NewClient("token")
	client.Client.Base = srv.URL
	if _, err := client.Organization("org").Pipeline("p").ListBuilds(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	snap, ok := client.Budget().Snapshot()
	if !ok {
		t.Fatal("the client made a request but recorded no budget")
	}
	if snap.Remaining != 41 || snap.Limit != 200 {
		t.Errorf("got %d/%d, want 41/200", snap.Remaining, snap.Limit)
	}
}
