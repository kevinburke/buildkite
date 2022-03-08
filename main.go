// The buildkite binary interacts with Buildkite.
//
// Usage:
//
//	buildkite command [arguments]
//
// The commands are:
//
//	version             Print the current version
//	wait                Wait for tests to finish on a branch.
//
// Use "buildkite help [command]" for more information about a command.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"time"

	buildkite "github.com/kevinburke/buildkite-go/lib"
	git "github.com/kevinburke/go-git"
)

const help = `The buildkite binary interacts with Buildkite CI.

Usage: 

	buildkite command [arguments]

The commands are:

	version             Print the current version
	wait                Wait for tests to finish on a branch.

Use "buildkite help [command]" for more information about a command.
`

const Version = "0.1"

func usage() {
	fmt.Fprintf(os.Stderr, help)
	flag.PrintDefaults()
}

func init() {
	flag.Usage = usage
}

func newClient(org string) (*buildkite.Client, error) {
	token, err := buildkite.GetToken(org)
	if err != nil {
		return nil, err
	}
	return buildkite.NewClient(token), nil
}

func main() {
	waitflags := flag.NewFlagSet("wait", flag.ExitOnError)
	waitRemote := waitflags.String("remote", "origin", "Git remote to use")
	waitflags.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: wait [refspec]

Wait for builds to complete, then print a descriptive output on success or
failure. By default, waits on the current branch, otherwise you can pass a
branch to wait for.

`)
		waitflags.PrintDefaults()
	}
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}
	subargs := args[1:]
	switch flag.Arg(0) {
	case "version":
		fmt.Fprintf(os.Stdout, "buildkite version %s\n", Version)
		os.Exit(0)
	case "wait":
		waitflags.Parse(subargs)
		args := waitflags.Args()
		branch, err := getBranchFromArgs(args)
		checkError(err, "getting git branch")
		err = doWait(branch, *waitRemote)
		checkError(err, "waiting for branch")
	default:
		fmt.Fprintf(os.Stderr, "buildkite: unknown command %q\n\n", flag.Arg(0))
		usage()
		os.Exit(2)
	}
}

func checkError(err error, msg string) {
	if err != nil {
		failError(err, msg)
	}
}

func failError(err error, msg string) {
	fmt.Fprintf(os.Stderr, "Error %s: %v\n", msg, err)
	os.Exit(1)
}

func getBuilds(ctx context.Context, client *buildkite.Client, org, repo, branch string) ([]buildkite.Build, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	builds, err := client.Organization(org).Pipeline(repo).ListBuilds(ctx, url.Values{
		"per_page": []string{"3"},
		"branch":   []string{branch},
	})
	if err != nil {
		return nil, err
	}
	fmt.Printf("%#v\n", builds)
	return builds, nil
}

var orgMap = map[string]string{
	"segmentio": "segment",
}

// isHttpError checks if the given error is a request timeout or a network
// failure - in those cases we want to just retry the request.
func isHttpError(err error) bool {
	if err == nil {
		return false
	}
	// some net.OpError's are wrapped in a url.Error
	if uerr, ok := err.(*url.Error); ok {
		err = uerr.Err
	}
	switch err := err.(type) {
	default:
		return false
	case *net.OpError:
		return err.Op == "dial" && err.Net == "tcp"
	case *net.DNSError:
		return true
	// Catchall, this needs to go last.
	case net.Error:
		return err.Timeout() || err.Temporary()
	}
}

var errNoBuilds = errors.New("buildkite: no builds")

func doWait(branch, remoteStr string) error {
	fmt.Println("wait for branch", branch, "remote", remoteStr)
	remote, err := git.GetRemoteURL(remoteStr)
	if err != nil {
		return err
	}
	tip, err := git.Tip(branch)
	if err != nil {
		return err
	}
	client, err := newClient(remote.Path)
	if err != nil {
		return err
	}
	fmt.Println("Waiting for latest build on", branch, "to complete")
	var lastPrintedAt time.Time
	org := remote.Path
	if bkOrg, ok := orgMap[org]; ok {
		org = bkOrg
	}
	var previousBuild *buildkite.Build
	builds, err := getBuilds(context.Background(), client, org, remote.RepoName, branch)
	if err == nil {
		for i := 1; i < len(builds); i++ {
			if builds[i].State == "passed" {
				previousBuild = &builds[i]
				break
			}
		}
	}
	for {
		latestBuild, err := getLatestBuild(client, remote.Path, remote.RepoName, branch)
		if err != nil {
			if isHttpError(err) {
				fmt.Printf("Caught network error: %s. Continuing\n", err.Error())
				lastPrintedAt = time.Now()
				time.Sleep(2 * time.Second)
				continue
			}
			if err == errNoBuilds {
				return fmt.Errorf("No results, are you sure there are tests for %s/%s?\n",
					remote.Path, remote.RepoName)
			}
			return err
		}
		_ = previousBuild
	}
	fmt.Println("tip", tip)
	fmt.Println("builds err", err)
	fmt.Println("builds", builds)
	_ = lastPrintedAt
	return nil
}
