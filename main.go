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
	openRemote := openflags.String("remote", "origin", "Git remote to use")
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

	switch flag.Arg(0) {
	case "wait":
		waitflags.Parse(subargs)
		args := waitflags.Args()
		branch, err := getBranchFromArgs(ctx, args)
		checkError(err, "getting git branch")

		remote, err := git.GetRemoteURL(*waitRemote)
		checkError(err, "loading git info")
		gitRemote := remote.Path
		org, ok := cfg.OrgForRemote(gitRemote)
		if !ok {
			slog.Warn("could not find a Buildkite org for remote", "remote", gitRemote)
			org = buildkite.Organization{
				Name: gitRemote,
			}
		}
		client, err := newClient(cfg, gitRemote)
		if err != nil {
			checkError(err, "creating Buildkite client")
		}

		err = doWait(ctx, client, org, remote, branch, *waitOutputLines)
		checkError(err, "waiting for branch")
	case "open":
		openflags.Parse(subargs)
		args := openflags.Args()
		branch, err := getBranchFromArgs(ctx, args)
		checkError(err, "getting git branch")

		remote, err := git.GetRemoteURL(*openRemote)
		checkError(err, "loading git info")
		gitRemote := remote.Path
		org, ok := cfg.OrgForRemote(gitRemote)
		if !ok {
			slog.Warn("could not find a Buildkite org for remote", "remote", gitRemote)
			org = buildkite.Organization{
				Name: gitRemote,
			}
		}
		client, err := newClient(cfg, gitRemote)
		if err != nil {
			checkError(err, "creating Buildkite client")
		}

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
	if msg == "" {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "Error %s: %v\n", msg, err)
	}
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
			candidates, err := findPipelineSlugs(ctx, client, orgName, slug)
			if err != nil {
				return err
			}
			foundSlug, err := tryPipelineCandidates(ctx, client, orgName, candidates, branch)
			if err != nil {
				return err
			}
			slug = foundSlug
		} else if err == errNoBuilds {
			// If original slug has no builds, try candidates
			candidates, err := findPipelineSlugs(ctx, client, orgName, slug)
			if err != nil {
				return err
			}
			foundSlug, err := tryPipelineCandidates(ctx, client, orgName, candidates, branch)
			if err != nil {
				return err
			}
			slug = foundSlug
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
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(2 * time.Second):
				}
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
	userRepo := strings.TrimSuffix(orgName+"/"+slug, "/")
	comparison = normalizeRepo(comparison)
	comparison = strings.TrimSuffix(comparison, "/")
	return comparison == userRepo || strings.HasSuffix(comparison, "/"+userRepo) || strings.HasSuffix(userRepo, "/"+comparison)
}

// longestCommonSuffix returns the length of the longest common suffix between two strings
func longestCommonSuffix(a, b string) int {
	i, j := len(a)-1, len(b)-1
	count := 0
	for i >= 0 && j >= 0 && a[i] == b[j] {
		count++
		i--
		j--
	}
	return count
}

// levenshteinDistance calculates the edit distance between two strings
func levenshteinDistance(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	matrix := make([][]int, len(a)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(b)+1)
		matrix[i][0] = i
	}
	for j := 0; j <= len(b); j++ {
		matrix[0][j] = j
	}

	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			matrix[i][j] = min(
				min(matrix[i-1][j]+1, matrix[i][j-1]+1), // deletion vs insertion
				matrix[i-1][j-1]+cost,                   // substitution
			)
		}
	}
	return matrix[len(a)][len(b)]
}

// repoSimilarityScore calculates a similarity score between the target repo and a candidate repo
// Higher scores indicate better matches
func repoSimilarityScore(orgName, slug, repoURL string) int {
	userRepo := strings.TrimSuffix(orgName+"/"+slug, "/")
	comparison := normalizeRepo(repoURL)
	comparison = strings.TrimSuffix(comparison, "/")

	// First check if it matches with the existing logic
	if sameRepo(orgName, slug, repoURL) {
		// Exact match (after normalization) gets highest score
		if comparison == userRepo {
			return 1000
		}

		// Check if comparison ends with our org/repo (like github.com/myorg/myrepo vs myorg/myrepo)
		if strings.HasSuffix(comparison, "/"+userRepo) {
			return 1000 // This is effectively exact match
		}

		// For partial matches using existing sameRepo logic, calculate similarity score
		score := 0

		// Longer common suffix gets higher score (up to 200 points)
		commonSuffix := longestCommonSuffix(userRepo, comparison)
		score += commonSuffix * 5

		// Same number of path segments gets bonus (50 points)
		if strings.Count(userRepo, "/") == strings.Count(comparison, "/") {
			score += 50
		}

		// Edit distance penalty - closer strings get higher scores (up to 100 points)
		editDist := levenshteinDistance(userRepo, comparison)
		maxLen := len(userRepo)
		if len(comparison) > maxLen {
			maxLen = len(comparison)
		}
		if maxLen > 0 {
			score += max(0, 100-(editDist*100/maxLen))
		}

		return score
	}

	// Enhanced logic for cases where repo name is contained within slug
	// Extract just the repo parts (after the last slash) from both userRepo and comparison
	userRepoParts := strings.Split(userRepo, "/")
	comparisonParts := strings.Split(comparison, "/")

	if len(userRepoParts) >= 2 && len(comparisonParts) >= 2 {
		repoName := userRepoParts[len(userRepoParts)-1]          // e.g., "internal-product-docs"
		candidateSlug := comparisonParts[len(comparisonParts)-1] // e.g., "twilio-internal-internal-product-docs"

		// Check if the repo name is contained in the candidate slug
		if strings.Contains(candidateSlug, repoName) {
			score := 500 // Base score for containing match

			// Bonus if it's at the end (more likely to be the same project)
			if strings.HasSuffix(candidateSlug, repoName) {
				score += 200
			}

			// Penalty based on extra characters (shorter extra parts = higher score)
			extraChars := len(candidateSlug) - len(repoName)
			if extraChars <= 10 {
				score += 100 - (extraChars * 5)
			}

			// Check if orgs match or are similar
			orgName = strings.ToLower(orgName)
			candidateOrg := strings.ToLower(comparisonParts[len(comparisonParts)-2])
			if orgName == candidateOrg {
				score += 100
			} else if strings.Contains(candidateOrg, orgName) || strings.Contains(orgName, candidateOrg) {
				score += 50
			}

			return score
		}
	}

	return 0 // No meaningful match found
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Result type to standardize return values
type Result struct {
	RequestType string
	Slug        string
	Error       error
}

// ScoredSlug represents a pipeline slug with its similarity score
type ScoredSlug struct {
	Slug  string
	Score int
}

func findPipelineSlugs(ctx context.Context, client *buildkite.Client, orgName, slug string) ([]ScoredSlug, error) {

	ctxA, cancelA := context.WithCancel(ctx)
	ctxB, cancelB := context.WithCancel(ctx)
	ctxC, cancelC := context.WithCancel(ctx)

	defer func() {
		cancelA()
		cancelB()
		cancelC()
	}()

	// Channel for all results
	type ResultWithCandidates struct {
		RequestType string
		Candidates  []ScoredSlug
		Error       error
		CanGraphQL  string
	}
	results := make(chan ResultWithCandidates)

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
		res := ResultWithCandidates{RequestType: "A", Error: err}
		if pipelines != nil {
			var candidates []ScoredSlug
			for _, p := range pipelines {
				score := repoSimilarityScore(orgName, slug, p.Repository)
				if score > 0 {
					candidates = append(candidates, ScoredSlug{Slug: p.Slug, Score: score})
				}
			}
			// Sort by score descending
			for i := 0; i < len(candidates)-1; i++ {
				for j := i + 1; j < len(candidates); j++ {
					if candidates[j].Score > candidates[i].Score {
						candidates[i], candidates[j] = candidates[j], candidates[i]
					}
				}
			}
			res.Candidates = candidates
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
		case results <- ResultWithCandidates{RequestType: "B", CanGraphQL: can, Error: err}:
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
		res := ResultWithCandidates{RequestType: "C"}

		var candidates []ScoredSlug
		for {
			resp, err := client.GraphQL().PipelineRepositoriesSlugs(ctxC, orgName, slug, parameters)
			if err != nil {
				res.Error = err
				break
			}
			if resp != nil {
				for _, node := range resp.Data.Organization.Pipelines.Edges {
					if node.Node.Repository.URL != "" {
						score := repoSimilarityScore(orgName, slug, node.Node.Repository.URL)
						if score > 0 {
							candidates = append(candidates, ScoredSlug{Slug: node.Node.Slug, Score: score})
						}
					}
				}
			}
			if !resp.Data.Organization.Pipelines.PageInfo.HasNextPage {
				break
			}
			parameters["after"] = resp.Data.Organization.Pipelines.PageInfo.EndCursor
		}

		// Sort by score descending
		for i := 0; i < len(candidates)-1; i++ {
			for j := i + 1; j < len(candidates); j++ {
				if candidates[j].Score > candidates[i].Score {
					candidates[i], candidates[j] = candidates[j], candidates[i]
				}
			}
		}
		res.Candidates = candidates
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
	var allCandidates []ScoredSlug
	canUseGraphQL := true
	for result := range results {
		if result.Error != nil {
			slog.Debug("got result.Error", "err", result.Error)
			continue
		}

		slog.Debug("request type", "type", result.RequestType, "candidates", len(result.Candidates))
		switch result.RequestType {
		case "A":
			if len(result.Candidates) > 0 {
				slog.Debug("Request A (ListPipelines) succeeded, cancelling B and C")
				cancelB()
				cancelC()
				allCandidates = result.Candidates
			}

		case "B":
			if result.CanGraphQL == "" {
				slog.Debug("Request B (Can) returned false, cancelling C")
				cancelC()
				canUseGraphQL = false
			}

		case "C":
			if len(result.Candidates) > 0 && canUseGraphQL {
				slog.Debug("Request C (GraphQLPipelines) succeeded, cancelling A and B")
				cancelA()
				cancelB()
				allCandidates = result.Candidates
			}
		}
	}

	if len(allCandidates) > 0 {
		return allCandidates, nil
	}
	return []ScoredSlug{}, fmt.Errorf("could not find pipeline slug for %q", slug)
}

// tryPipelineCandidates tries each pipeline candidate in order until it finds one with builds
func tryPipelineCandidates(ctx context.Context, client *buildkite.Client, orgName string, candidates []ScoredSlug, branch string) (string, error) {
	for i, candidate := range candidates {
		slog.Debug("Trying pipeline candidate", "slug", candidate.Slug, "score", candidate.Score, "attempt", i+1, "total", len(candidates))
		_, err := getLatestBuild(ctx, client, orgName, candidate.Slug, branch)
		if err == nil {
			slog.Debug("Found builds for pipeline", "slug", candidate.Slug)
			return candidate.Slug, nil
		}
		if err != errNoBuilds {
			// For non-"no builds" errors, continue to next candidate
			slog.Debug("Non-build error for candidate", "slug", candidate.Slug, "err", err)
			continue
		}
		slog.Debug("No builds found for candidate", "slug", candidate.Slug)
	}
	return "", fmt.Errorf("no pipeline candidates have builds for branch %s", branch)
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
			candidates, err := findPipelineSlugs(ctx, client, orgName, slug)
			if err != nil {
				return err
			}
			foundSlug, err := tryPipelineCandidates(ctx, client, orgName, candidates, branch)
			if err != nil {
				return err
			}
			slug = foundSlug
		} else if err == errNoBuilds {
			// If original slug has no builds, try candidates
			candidates, err := findPipelineSlugs(ctx, client, orgName, slug)
			if err != nil {
				return err
			}
			foundSlug, err := tryPipelineCandidates(ctx, client, orgName, candidates, branch)
			if err != nil {
				return err
			}
			slug = foundSlug
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
