package main

import (
	"context"

	git "github.com/kevinburke/go-git"
)

// Given a set of command line args, return the git branch or an error. Returns
// the current git branch if no argument is specified
func getBranchFromArgs(ctx context.Context, args []string) (string, error) {
	if len(args) == 0 {
		return git.CurrentBranch(ctx)
	} else {
		return args[0], nil
	}
}
