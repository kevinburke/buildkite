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

	"github.com/kevinburke/bigtext"
	buildkite "github.com/kevinburke/buildkite/lib"
	git "github.com/kevinburke/go-git"
	"github.com/pkg/browser"
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

func usage() {
	fmt.Fprint(os.Stderr, help)
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
	waitOutputLines := waitflags.Int("failed-output-lines", 100, "Number of lines of failed output to display")
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
		fmt.Fprintf(os.Stdout, "buildkite version %s\n", buildkite.Version)
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
		err = doWait(ctx, client, org, remote, branch, *waitOutputLines)
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

func getAnnotations(ctx context.Context, client *buildkite.Client, org, repo string, build int64) (buildkite.AnnotationResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return client.Organization(org).Pipeline(repo).Build(build).Annotations(ctx, nil)
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
		return err.Timeout()
	}
}

var errNoBuilds = errors.New("buildkite: no builds")

func shouldPrint(lastPrinted time.Time, duration time.Duration, latestBuild buildkite.Build, previousBuild *buildkite.Build) bool {
	_ = latestBuild
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
	_ = flags
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

func doWait(ctx context.Context, client *buildkite.Client, org buildkite.Organization, remote *git.RemoteURL, branch string, numOutputLines int) error {
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
				select {
				case <-ctx.Done():
					return err
				case <-time.After(2 * time.Second):
				}
				continue
			}
			if err == errNoBuilds {
				return fmt.Errorf("No results, are you sure there are tests for %s/%s?\n",
					org.Name, remote.RepoName)
			}
			return err
		}
		if latestBuild.Commit != tip {
			fmt.Printf("Latest build in Buildkite is %s, waiting for %s...\n",
				latestBuild.Commit, tip)
			lastPrintedAt = time.Now()
			select {
			case <-ctx.Done():
				return err
			case <-time.After(5 * time.Second):
			}
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
			// TODO
			var annotationANSI []string
			annotations, err := getAnnotations(ctx, client, org.Name, remote.RepoName, latestBuild.Number)
			if err == nil {
				annotationANSI, _ = getANSIAnnotations(annotations)
			}
			data := client.BuildSummary(ctx, org.Name, latestBuild, numOutputLines)
			os.Stdout.Write(data)
			output := fmt.Sprintf("\nTests on %s took %s. Quitting.\n", branch, duration.String())
			if latestBuild.PullRequest != nil {
				// No prefix for the URL so you can click and copy the whole
				// line easily
				output += latestBuild.PullRequest.URL() + "\n"
			}
			if len(annotationANSI) > 0 {
				output += "\nAnnotations:\n"
				for _, annotation := range annotationANSI {
					output += annotation + "\n"
				}
			}
			fmt.Print(output)
			c.Display(branch + " build complete!")
			return nil
		case "failing", "failed":
			data := client.BuildSummary(ctx, org.Name, latestBuild, numOutputLines)
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
			fmt.Printf("\nURL:\n%s\n", latestBuild.WebURL)
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
			/*
				if latestBuild.State == "failing" {
					fmt.Printf("latest build: %#v\n", latestBuild)
				}
			*/
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
