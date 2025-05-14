package lib

import (
	"context"
	"testing"
)

func TestGraphQL(t *testing.T) {
	ctx := context.Background()
	cfg, err := LoadConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	token, err := cfg.Token("twilio")
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(token)
	can, err := client.CanGraphQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if can != true {
		t.Fatal("expected to be able to use GraphQL, but was not able to")
	}
}
