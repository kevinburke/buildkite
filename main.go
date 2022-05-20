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
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/juju/ansiterm/tabwriter"
	"github.com/kevinburke/bigtext"
	buildkite "github.com/kevinburke/buildkite/lib"
	git "github.com/kevinburke/go-git"
	"github.com/pkg/browser"
	"golang.org/x/term"
)

const help = `The buildkite binary interacts with Buildkite CI.

Usage: 

	buildkite command [arguments]

The commands are:

	open                Open the running build in your browser
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

func newClient(cfg *buildkite.FileConfig, gitRemote string) (*buildkite.Client, error) {
	token, err := cfg.Token(gitRemote)
	if err != nil {
		return nil, err
	}
	return buildkite.NewClient(token), nil
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waitflags := flag.NewFlagSet("wait", flag.ExitOnError)
	openflags := flag.NewFlagSet("open", flag.ExitOnError)
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
	mainArgs := flag.Args()
	if len(mainArgs) < 1 {
		usage()
		os.Exit(2)
	}
	subargs := mainArgs[1:]
	if flag.Arg(0) == "version" {
		fmt.Fprintf(os.Stdout, "buildkite version %s\n", Version)
		os.Exit(0)
	}
	cfg, err := buildkite.LoadConfig(ctx)
	checkError(err, "loading buildkite config")
	remote, err := git.GetRemoteURL(*waitRemote)
	checkError(err, "loading git info")
	gitRemote := remote.Path
	org, ok := cfg.OrgForRemote(gitRemote)
	if !ok {
		checkError(fmt.Errorf("could not find a Buildkite org for remote %q", gitRemote), "")
	}
	client, err := newClient(cfg, gitRemote)
	if err != nil {
		checkError(err, "creating Buildkite client")
	}
	switch flag.Arg(0) {
	case "wait":
		waitflags.Parse(subargs)
		args := waitflags.Args()
		branch, err := getBranchFromArgs(args)
		checkError(err, "getting git branch")
		err = doWait(ctx, client, org, remote, branch)
		checkError(err, "waiting for branch")
	case "open":
		openflags.Parse(subargs)
		args := openflags.Args()
		branch, err := getBranchFromArgs(args)
		checkError(err, "getting git branch")
		checkError(doOpen(ctx, openflags, client, org, remote, branch), "opening build")
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
	return builds, nil
}

func getLatestBuild(ctx context.Context, client *buildkite.Client, org, repo, branch string) (buildkite.Build, error) {
	builds, err := getBuilds(ctx, client, org, repo, branch)
	if err != nil {
		return buildkite.Build{}, err
	}
	if len(builds) == 0 {
		return buildkite.Build{}, errNoBuilds
	}
	return builds[0], nil
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

// getMinTipLength compares two strings and returns the length of the
// shortest
func getMinTipLength(remoteTip string, localTip string) int {
	var minTipLength int
	if len(remoteTip) <= len(localTip) {
		minTipLength = len(remoteTip)
	} else {
		minTipLength = len(localTip)
	}
	return minTipLength
}

func shouldPrint(lastPrinted time.Time, duration time.Duration, latestBuild buildkite.Build, previousBuild *buildkite.Build) bool {
	now := time.Now()
	var buildDuration time.Duration
	if previousBuild == nil {
		buildDuration = 5 * time.Minute
	} else {
		buildDuration = previousBuild.FinishedAt.Time.Sub(previousBuild.StartedAt)
	}
	var durToUse time.Duration
	timeRemaining := buildDuration - duration
	switch {
	case timeRemaining > 25*time.Minute:
		durToUse = 3 * time.Minute
	case timeRemaining > 8*time.Minute:
		durToUse = 2 * time.Minute
	case timeRemaining > 5*time.Minute:
		durToUse = 30 * time.Second
	case timeRemaining > 3*time.Minute:
		durToUse = 20 * time.Second
	case timeRemaining > time.Minute:
		durToUse = 15 * time.Second
	default:
		durToUse = 10 * time.Second
	}
	return lastPrinted.Add(durToUse).Before(now)
}

func doOpen(ctx context.Context, flags *flag.FlagSet, client *buildkite.Client, org buildkite.Organization, remote *git.RemoteURL, branch string) error {
	tip, err := git.Tip(branch)
	if err != nil {
		return err
	}
	for {
		latestBuild, err := getLatestBuild(ctx, client, org.Name, remote.RepoName, branch)
		if err != nil {
			if isHttpError(err) {
				fmt.Printf("Caught network error: %s. Continuing\n", err.Error())
				time.Sleep(2 * time.Second)
				continue
			}
			if err == errNoBuilds {
				return fmt.Errorf("No results, are you sure there are tests for %s/%s?\n",
					remote.Path, remote.RepoName)
			}
			return err
		}
		if latestBuild.Commit != tip {
			fmt.Printf("Latest build in Buildkite is %s, waiting for %s...\n",
				latestBuild.Commit, tip)
			time.Sleep(5 * time.Second)
			continue
		}
		if err := browser.OpenURL(latestBuild.WebURL); err != nil {
			return err
		}
		return nil
	}
}

func buildSummary(build buildkite.Build) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{'\n'}) // the end of the '=' line
	writer := tabwriter.NewWriter(&buf, 0, 0, 1, ' ', 0)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	_ = ctx
	defer cancel()
	/*
		build, err := client.Builds.Get(ctx, latestBuild.ID, "build.jobs", "job.config")
		if err == nil {
			stats, err := client.BuildSummary(ctx, build)
			if err == nil {
				fmt.Print(stats)
			} else {
				fmt.Printf("error fetching build summary: %v\n", err)
			}
		} else {
			fmt.Printf("error getting build: %v\n", err)
		}
	*/
	for i := range build.Jobs {
		duration := build.Jobs[i].FinishedAt.Time.Sub(build.Jobs[i].StartedAt)
		if duration > time.Minute {
			duration = duration.Round(time.Second)
		} else {
			duration = duration.Round(10 * time.Millisecond)
		}
		var durString string
		if build.Jobs[i].Failed() && isatty() {
			durString = fmt.Sprintf("\033[38;05;160m%-8s\033[0m", duration.String())
		} else {
			durString = duration.String()
		}
		fmt.Fprintf(writer, "%s\t%s\n", build.Jobs[i].Name, durString)
	}
	writer.Flush()
	linelen := bytes.IndexByte(buf.Bytes()[1:], '\n')
	var buf2 bytes.Buffer
	buf2.WriteByte('\n')
	buf2.Write(bytes.Repeat([]byte{'='}, linelen))
	return append(buf.Bytes(), buf2.Bytes()...)
}

func isatty() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func doWait(ctx context.Context, client *buildkite.Client, org buildkite.Organization, remote *git.RemoteURL, branch string) error {
	tip, err := git.Tip(branch)
	if err != nil {
		return err
	}
	fmt.Println("Waiting for latest build on", branch, "to complete")
	var lastPrintedAt time.Time
	var previousBuild *buildkite.Build
	builds, err := getBuilds(ctx, client, org.Name, remote.RepoName, branch)
	if err == nil {
		for i := 1; i < len(builds); i++ {
			if builds[i].State == "passed" {
				previousBuild = &builds[i]
				break
			}
		}
	}
	done := false
	for !done {
		latestBuild, err := getLatestBuild(ctx, client, org.Name, remote.RepoName, branch)
		if err != nil {
			if isHttpError(err) {
				fmt.Printf("Caught network error: %s. Continuing\n", err.Error())
				lastPrintedAt = time.Now()
				time.Sleep(2 * time.Second)
				continue
			}
			if err == errNoBuilds {
				return fmt.Errorf("No results, are you sure there are tests for %s/%s?\n",
					org, remote.RepoName)
			}
			return err
		}
		if latestBuild.Commit != tip {
			fmt.Printf("Latest build in Buildkite is %s, waiting for %s...\n",
				latestBuild.Commit, tip)
			lastPrintedAt = time.Now()
			time.Sleep(5 * time.Second)
			continue
		}
		var duration time.Duration
		if latestBuild.FinishedAt.Valid {
			duration = latestBuild.FinishedAt.Time.Sub(latestBuild.StartedAt).Round(time.Second)
		} else {
			duration = time.Since(latestBuild.StartedAt).Round(time.Second)
		}
		c := bigtext.Client{
			Name: "buildkite (" + remote.RepoName + ")",
		}
		switch latestBuild.State {
		case "passed":
			data := buildSummary(latestBuild)
			os.Stdout.Write(data)
			fmt.Printf("\nTests on %s took %s. Quitting.\n", branch, duration.String())
			c.Display(branch + " build complete!")
			return nil
		case "failed":
			data := buildSummary(latestBuild)
			os.Stdout.Write(data)
			/*
				build, err := getBuild(client, latestBuild.ID)
				if err == nil {
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer cancel()
					stats, err := client.BuildSummary(ctx, build)
					if err == nil {
						fmt.Print(stats)
					} else {
						fmt.Printf("error fetching build stats: %v\n", err)
					}
				} else {
					fmt.Printf("error getting build: %v\n", err)
				}
			*/
			fmt.Printf("\nURL: %s\n", latestBuild.WebURL)
			err = fmt.Errorf("Build on %s failed!\n\n", branch)
			c.Display("build failed")
			return err
		case "running":
			// Show more and more output as we approach the duration of the previous
			// successful build.
			if shouldPrint(lastPrintedAt, duration, latestBuild, previousBuild) {
				fmt.Printf("Build %d running (%s elapsed)\n", latestBuild.Number, duration.String())
				lastPrintedAt = time.Now()
			}
		default:
			fmt.Printf("State is %s, trying again\n", latestBuild.State)
			lastPrintedAt = time.Now()
		}
		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
		_ = previousBuild
	}
	/*
		fmt.Println("tip", tip)
		fmt.Println("builds err", err)
		fmt.Println("builds", builds)
	*/
	_ = lastPrintedAt
	return nil
}
