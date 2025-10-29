//go:build darwin

package main

import (
	"os"
	"os/exec"
	"strings"

	buildkite "github.com/kevinburke/buildkite/lib"
)

func runCmd(prog string, args ...string) error {
	cmd := exec.Command(prog, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func openURL(org buildkite.Organization, url string) error {
	args := []string{}
	if org.BrowserApplication != "" {
		browserLower := strings.ToLower(org.BrowserApplication)

		if strings.Contains(browserLower, "firefox") && org.BrowserProfile != "" {
			// Firefox with profile: open -n -a "Firefox" --args -no-remote -P "profile-name" -new-tab "url"
			args = append(args, "-n", "-a", org.BrowserApplication)
			args = append(args, "--args", "-no-remote", "-P", org.BrowserProfile, "-new-tab")
		} else if org.BrowserProfile != "" {
			// Chrome-like browsers with profile: open -na "Chrome" --args --profile-directory="Profile" --new-tab "url"
			args = append(args, "-na", org.BrowserApplication)
			args = append(args, "--args", "--profile-directory="+org.BrowserProfile, "--new-tab")
		} else {
			// Just browser application, no profile
			args = append(args, "-a", org.BrowserApplication)
		}
	}
	args = append(args, url)
	return runCmd("open", args...)
}
