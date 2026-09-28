package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	buildkite "github.com/kevinburke/buildkite/lib"
)

func TestJobLogFilename(t *testing.T) {
	tests := []struct {
		i    int
		name string
		want string
	}{
		{0, ":go: Test", "01-go-test.log"},
		{9, "lint & vet (linux/amd64)", "10-lint-vet-linux-amd64.log"},
		{1, ":pipeline:", "02-pipeline.log"},
		{2, "", "03-job.log"},
		{3, "../../etc/passwd", "04-etc-passwd.log"},
		{4, strings.Repeat("a", 100), "05-" + strings.Repeat("a", 60) + ".log"},
	}
	for _, tt := range tests {
		if got := jobLogFilename(tt.i, tt.name); got != tt.want {
			t.Errorf("jobLogFilename(%d, %q) = %q, want %q", tt.i, tt.name, got, tt.want)
		}
	}
}

func TestWriteJobLogs(t *testing.T) {
	started := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	build := buildkite.Build{
		Number: 42,
		Jobs: []buildkite.Job{
			{ID: "a", Name: "test", State: "passed", StartedAt: started, LogURL: "u"},
			{ID: "wait", State: "passed"}, // waiter step, no log
			{ID: "b", Name: "lint", State: "failed", StartedAt: started, LogURL: "u"},
			{ID: "c", Name: "deploy", State: "scheduled", LogURL: "u"},
		},
	}
	logs := map[string]string{
		"a": "\x1b_bk;t=1700000000000\x07ok \x1b[32mgreen\x1b[0m\n",
		"b": "FAIL\n",
	}
	fetch := func(ctx context.Context, id string) ([]byte, error) {
		l, ok := logs[id]
		if !ok {
			t.Errorf("unexpected fetch of job %q", id)
		}
		return []byte(l), nil
	}

	dir := t.TempDir()
	var stderr bytes.Buffer
	n, err := writeJobLogs(context.Background(), fetch, build, dir, logsOptions{}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("wrote %d files, want 2", n)
	}
	got, err := os.ReadFile(filepath.Join(dir, "01-test.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ok green\n" {
		t.Errorf("01-test.log = %q, want escapes stripped", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "03-lint.log")); err != nil {
		t.Error(err)
	}
	if !strings.Contains(stderr.String(), "not started") {
		t.Errorf("expected the unstarted job to be reported, got:\n%s", stderr.String())
	}

	// --failed and --raw
	dir = t.TempDir()
	n, err = writeJobLogs(context.Background(), fetch, build, dir, logsOptions{FailedOnly: true, Raw: true}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("wrote %d files with FailedOnly, want 1", n)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "03-lint.log" {
		t.Errorf("unexpected files with FailedOnly: %v", entries)
	}
}

func TestWriteJobLogsReportsFailures(t *testing.T) {
	started := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	build := buildkite.Build{
		Number: 7,
		Jobs: []buildkite.Job{
			{ID: "a", Name: "one", State: "passed", StartedAt: started, LogURL: "u"},
			{ID: "b", Name: "two", State: "passed", StartedAt: started, LogURL: "u"},
		},
	}
	fetch := func(ctx context.Context, id string) ([]byte, error) {
		if id == "a" {
			return nil, errors.New("boom")
		}
		return []byte("two\n"), nil
	}
	dir := t.TempDir()
	n, err := writeJobLogs(context.Background(), fetch, build, dir, logsOptions{}, new(bytes.Buffer))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected download error to be returned, got %v", err)
	}
	if n != 1 {
		t.Errorf("wrote %d files, want the job that succeeded to still be written", n)
	}
}

func TestWriteJobLogsNoFailedJobs(t *testing.T) {
	build := buildkite.Build{
		Number: 7,
		Jobs: []buildkite.Job{
			{ID: "a", Name: "one", State: "passed", StartedAt: time.Now(), LogURL: "u"},
		},
	}
	fetch := func(ctx context.Context, id string) ([]byte, error) { return nil, nil }
	_, err := writeJobLogs(context.Background(), fetch, build, t.TempDir(), logsOptions{FailedOnly: true}, new(bytes.Buffer))
	if err == nil || !strings.Contains(err.Error(), "no failed jobs") {
		t.Fatalf("expected a 'no failed jobs' error, got %v", err)
	}
}
