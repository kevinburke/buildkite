package lib

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestBuildFailure(t *testing.T) {
	t.Skip("add an actual assertion here.")
	var log Log
	if err := json.Unmarshal(logResponse, &log); err != nil {
		t.Fatal(err)
	}
	byteContent := []byte(log.Content)
	if len(byteContent) < 1000 {
		t.Error("did not unmarshal correct build object")
	}
	out := FindBuildFailure(byteContent, 10)
	fmt.Printf("output: %q\n", string(out))
	fmt.Printf("output: %v\n", string(out))
	t.Fail()
}

func TestFindBuildFailureReturnsEndWhenNoPostHook(t *testing.T) {
	// Simulate a log without post-command hook - should return the last lines
	logContent := `~~~ Preparing working directory
$ cd /path/to/build
$ git fetch
$ git checkout
~~~ Running commands
$ make test

staticcheck ./... || true
some/file.go:10:3: error strings should not be capitalized (ST1005)
go vet ./... || true
go test -v ./...
=== RUN   TestSomething
--- PASS: TestSomething (0.00s)
=== RUN   TestFailingTest
    test.go:42: assertion failed
--- FAIL: TestFailingTest (0.00s)
FAIL
FAIL	example.com/project	0.123s
FAIL
make: *** [test] Error 1
user command error: exit status 2`

	result := FindBuildFailure([]byte(logContent), 5)
	resultStr := string(result)

	// Should extract the last 5 lines which contain the failure
	expectedLines := []string{
		"FAIL	example.com/project	0.123s",
		"FAIL",
		"make: *** [test] Error 1",
		"user command error: exit status 2",
	}

	for _, expected := range expectedLines {
		if !strings.Contains(resultStr, expected) {
			t.Errorf("Expected result to contain %q, but got: %q", expected, resultStr)
		}
	}

	// Should NOT contain the early parts of the log
	if strings.Contains(resultStr, "staticcheck") {
		t.Errorf("Should not contain early log content, but got: %q", resultStr)
	}
}

func TestFindBuildFailureWithPostHook(t *testing.T) {
	// Simulate a log WITH post-command hook - should return content before hook
	logContent := `~~~ Running commands
$ make test
go test -v ./...
=== RUN   TestSomething
--- PASS: TestSomething (0.00s)
=== RUN   TestFailingTest
    test.go:42: assertion failed
--- FAIL: TestFailingTest (0.00s)
FAIL
FAIL	example.com/project	0.123s
FAIL
~~~ Running repository post-command hook
$ cleanup.sh
Cleaning up artifacts...
Done.`

	result := FindBuildFailure([]byte(logContent), 5)
	resultStr := string(result)

	// Should extract content before the post-command hook
	expectedLines := []string{
		"--- FAIL: TestFailingTest (0.00s)",
		"FAIL",
		"FAIL	example.com/project	0.123s",
	}

	for _, expected := range expectedLines {
		if !strings.Contains(resultStr, expected) {
			t.Errorf("Expected result to contain %q, but got: %q", expected, resultStr)
		}
	}

	// Should NOT contain post-hook content
	if strings.Contains(resultStr, "cleanup.sh") || strings.Contains(resultStr, "Cleaning up") {
		t.Errorf("Should not contain post-hook content, but got: %q", resultStr)
	}
}

var commandTests = []struct {
	in   string
	want bool
}{
	{"~~~ Running command", true},
	{"~~~ Running commandsblah", false},
	{"~~~ Running script", true},
	{"~~~ Running batch script", true},
	{"~~~ Running batch foo script", false},
	{"~~~ Running global command", true},
	{"~~~ Running local command", true},
	{"~~~ Running plugin command", true},
	{"~~~ Running plugin commands", false},
}

func TestCommandRe(t *testing.T) {
	for _, tt := range commandTests {
		got := runCommandRe.MatchString(tt.in)
		if got != tt.want {
			t.Errorf("MatchString(%q): got %t, want %t", tt.in, got, tt.want)
		}
	}
}

func TestPullRequest(t *testing.T) {
	p := PullRequest{ID: "421", Base: "main", Repository: "https://github.com/segmentio/integrations-consumer.git"}
	if u := p.URL(); u != "https://github.com/segmentio/integrations-consumer/pull/421" {
		t.Errorf("incorrect URL: got %q", u)
	}
}
