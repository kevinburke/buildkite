package lib

import (
	"encoding/json"
	"fmt"
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

var commandTests = []struct {
	in   string
	want bool
}{
	{"~~~ Running command", true},
	{"~~~ Running commands\r\n", true},
	{"~~~ Running commands another word\r\n", true},
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

	t.Error("The build failed")
}
