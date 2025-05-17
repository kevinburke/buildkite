//go:build darwin

package main

import (
	"os"
	"os/exec"

	buildkite "github.com/kevinburke/buildkite/lib"
)

func runCmd(prog string, args ...string) error {
	cmd := exec.Command(prog, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func openURL(org buildkite.Organization, url string) error {
	// open -a "Google Chrome" --args --profile-directory="Profile 4" --new-tab "https://example.com"
	args := []string{}
	if org.BrowserApplication != "" {
		// "-n" == "new instance"
		args = append(args, "-na", org.BrowserApplication)
		args = append(args, "--args")

		if org.BrowserProfile != "" {
			args = append(args, "--profile-directory="+org.BrowserProfile)
		}
	}
	args = append(args, url)
	return runCmd("open", args...)
}
