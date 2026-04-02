//go:build !darwin

package main

import (
	"os"
	"os/exec"
	"runtime"

	buildkite "github.com/kevinburke/buildkite/lib"
)

func openURL(_ buildkite.Organization, url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
