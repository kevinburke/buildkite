package emoji_test

import (
	"fmt"
	"testing"

	"github.com/kevinburke/buildkite/emoji"
)

func TestExport(t *testing.T) {
	l := emoji.NewLoader()
	s := l.Render(":pipeline:  :javascript:  foo bar bang")
	fmt.Printf("s: %s\n", s)
	t.Fail()
}
