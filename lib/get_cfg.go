package lib

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func checkFile(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, os.ErrNotExist)
}

// Check for the following config paths:
// - $XDG_CONFIG_HOME/buildkite
// - $HOME/cfg/buildkite
// - $HOME/.buildkite

func getCfgPath() (string, error) {

	checkedLocations := make([]string, 0)

	xdgPath, ok := os.LookupEnv("XDG_CONFIG_HOME")
	filePath := filepath.Join(xdgPath, "buildkite")
	checkedLocations = append(checkedLocations, filePath)
	if ok && checkFile(filePath) {
		return filePath, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	cfgPath := filepath.Join(homeDir, "cfg", "buildkite")
	checkedLocations = append(checkedLocations, cfgPath)
	if checkFile(cfgPath) {
		return cfgPath, nil
	}

	localPath := filepath.Join(homeDir, ".buildkite")
	checkedLocations = append(checkedLocations, localPath)
	if checkFile(localPath) {
		return localPath, nil
	}

	return "", //lint:ignore ST1005 this shows up in public facing error.
		fmt.Errorf(`Couldn't find a config file in %s.

Add a configuration file with your Buildkite token, like this:

[organizations]

    [organizations.buildkite_org]
    token = "aabbccddeeff00"
    git_remotes = [ "github_org" ]

Go to https://buildkite.com/user/api-access-tokens if you need to find your token.
`, strings.Join(checkedLocations, " or "))
}
