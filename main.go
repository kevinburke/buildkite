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
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kevinburke/bigtext"
	buildkite "github.com/kevinburke/buildkite/lib"
	git "github.com/kevinburke/go-git"
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
	debug := flag.Bool("debug", false, "Enable the debug log level")
	flag.Parse()
	if *debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
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
		branch, err := getBranchFromArgs(ctx, args)
		checkError(err, "getting git branch")
		err = doWait(ctx, client, org, remote, branch, *waitOutputLines)
		checkError(err, "waiting for branch")
	case "open":
		openflags.Parse(subargs)
		args := openflags.Args()
		branch, err := getBranchFromArgs(ctx, args)
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
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
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
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
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
	orgName, slug := org.Name, remote.RepoName
	_, err = getLatestBuild(ctx, client, orgName, slug, branch)
	if err != nil {
		if berr, ok := err.(*buildkite.Error); ok && berr.StatusCode == 404 {
			pipelineSlug, err := findPipelineSlug(ctx, client, orgName, slug)
			if err != nil {
				return err
			}
			slug = pipelineSlug
		} else {
			fmt.Printf("latest build at top of doWait err: %v\n", err)
			return err
		}
	}

	for {
		latestBuild, err := getLatestBuild(ctx, client, orgName, slug, branch)
		if err != nil {
			if isHttpError(err) {
				fmt.Printf("Caught network error: %s. Continuing\n", err.Error())
				time.Sleep(2 * time.Second)
				continue
			}
			if err == errNoBuilds {
				//lint:ignore ST1005 this shows up in public facing error.
				return fmt.Errorf("No results, are you sure there are tests for %s/%s?\n",
					remote.Path, remote.RepoName)
			}
			return err
		}
		if latestBuild.Commit != tip {
			fmt.Printf("Latest build in Buildkite is %s, waiting for %s...\n",
				latestBuild.Commit, tip)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
			continue
		}
		if err := openURL(org, latestBuild.WebURL); err != nil {
			return err
		}
		return nil
	}
}

// this just turns everything into e.g. github.com/user/repo stripping leading
// and trailing info.
func normalizeRepo(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimSuffix(u, ".git")
	u = strings.TrimSuffix(u, "/")
	// git@github.com:foo/bar -> github.com/foo/bar
	if strings.HasPrefix(u, "git@") {
		u = strings.TrimPrefix(u, "git@")
		u = strings.Replace(u, ":", "/", 1)
	}
	for _, p := range []string{"https://", "http://", "ssh://"} {
		u = strings.TrimPrefix(u, p)
	}
	return strings.ToLower(u)
}

func sameRepo(orgName, slug, comparison string) bool {
	// can't use the org name => github org name because they might not match,
	// by default.
	// unfortunately this means forks will match

	// userRepo := strings.TrimSuffix(orgName+"/"+slug, "/")
	// comparison = strings.TrimSuffix(comparison, "/")
	// return comparison == userRepo || strings.HasSuffix(comparison, "/"+userRepo) || strings.HasSuffix(userRepo, "/"+comparison)
	return strings.HasSuffix(comparison, slug)
}

// Result type to standardize return values
type Result struct {
	RequestType string
	Slug        string
	Error       error
}

func findPipelineSlug(ctx context.Context, client *buildkite.Client, orgName, slug string) (string, error) {

	ctxA, cancelA := context.WithCancel(ctx)
	ctxB, cancelB := context.WithCancel(ctx)
	ctxC, cancelC := context.WithCancel(ctx)

	defer func() {
		cancelA()
		cancelB()
		cancelC()
	}()

	// Channel for all results
	results := make(chan Result)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		const perPage = 100
		page := 1

		defer wg.Done()
		data := url.Values{}
		data.Set("per_page", strconv.Itoa(perPage))
		data.Set("page", strconv.Itoa(page))
		pipelines, err := client.Organization(orgName).ListPipelines(ctxA, data)
		res := Result{RequestType: "A", Error: err}
		if pipelines == nil {
			for _, p := range pipelines {
				if sameRepo(orgName, slug, normalizeRepo(p.Repository)) {
					res.Slug = p.Slug
				}
			}
		}
		select {
		case results <- res:
		case <-ctxA.Done():
			// Context was cancelled, don't send result
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		success, err := client.GraphQL().Can(ctxB)
		can := ""
		if success {
			can = "can"
		}
		select {
		case results <- Result{"B", can, err}:
		case <-ctxB.Done():
			// Context was cancelled, don't send result
		}
	}()

	// Start request C: GraphQLPipelines
	wg.Add(1)
	go func() {
		defer wg.Done()
		parameters := map[string]interface{}{
			"first": 250,
		}
		found := false
		res := Result{RequestType: "C"}
		for !found {
			resp, err := client.GraphQL().PipelineRepositoriesSlugs(ctxC, orgName, slug, parameters)
			if err != nil {
				res.Error = err
				break
			}
			if resp != nil {
				for _, node := range resp.Data.Organization.Pipelines.Edges {
					if node.Node.Repository.URL != "" {
						normalized := normalizeRepo(node.Node.Repository.URL)
						s := sameRepo(orgName, slug, normalized)
						slog.Debug("checking repo", "org", orgName, "slug", slug, "normalized", normalized, "same", s)
						if s {
							res.Slug = node.Node.Slug
							found = true
							break
						}
					}
				}
			}
			if !resp.Data.Organization.Pipelines.PageInfo.HasNextPage {
				break
			}
			parameters["after"] = resp.Data.Organization.Pipelines.PageInfo.EndCursor
		}
		select {
		case results <- res:
		case <-ctxC.Done():
			// Context was cancelled, don't send result
		}
	}()

	// Create a goroutine to close the results channel when all requests are done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Process results and apply cancellation rules
	bkSlug := ""
	for result := range results {
		if result.Error != nil {
			slog.Debug("got result.Error", "err", result.Error)
			continue
		}

		slog.Debug("request type", "type", result.RequestType, "slug", result.Slug)
		switch result.RequestType {
		case "A":
			if result.Slug != "" {
				slog.Debug("Request A (ListPipelines) succeeded, cancelling B and C")
				cancelB()
				cancelC()
				bkSlug = result.Slug
			}

		case "B":
			if result.Slug == "" {
				slog.Debug("Request B (Can) returned false, cancelling C")
				cancelC()
			}

		case "C":
			if result.Slug != "" {
				slog.Debug("Request C (GraphQLPipelines) succeeded, cancelling A and B")
				cancelA()
				cancelB()
				bkSlug = result.Slug
			}
		}
	}

	if bkSlug != "" {
		return bkSlug, nil
	}
	return "", fmt.Errorf("could not find pipeline slug for %q", slug)
}

func doWait(ctx context.Context, client *buildkite.Client, org buildkite.Organization, remote *git.RemoteURL, branch string, numOutputLines int) error {
	tip, err := git.Tip(branch)
	if err != nil {
		return err
	}
	orgName, slug := org.Name, remote.RepoName

	_, err = getLatestBuild(ctx, client, orgName, slug, branch)
	if err != nil {
		if berr, ok := err.(*buildkite.Error); ok && berr.StatusCode == 404 {
			pipelineSlug, err := findPipelineSlug(ctx, client, orgName, slug)
			if err != nil {
				return err
			}
			slug = pipelineSlug
		} else {
			fmt.Printf("latest build at top of doWait err: %v\n", err)
			return err
		}
	}

	fmt.Println("Waiting for latest build on", branch, "to complete")
	var lastPrintedAt time.Time
	var previousBuild *buildkite.Build
	builds, err := getBuilds(ctx, client, orgName, slug, branch)
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
		latestBuild, err := getLatestBuild(ctx, client, orgName, slug, branch)
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
				//lint:ignore ST1005 this shows up in public facing error.
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
			annotations, err := getAnnotations(ctx, client, orgName, slug, latestBuild.Number)
			if err == nil {
				annotationANSI, _ = getANSIAnnotations(annotations)
			}
			data := client.BuildSummary(ctx, orgName, latestBuild, numOutputLines)
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
			data := client.BuildSummary(ctx, orgName, latestBuild, numOutputLines)
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
			//lint:ignore ST1005 this shows up in public facing error.
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
