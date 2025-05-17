//go:build !darwin

package main

import buildkite "github.com/kevinburke/buildkite/lib"

func openURL(org buildkite.Organization, url string) {
	return browser.Open(url)
}
