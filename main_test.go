package main

import (
	"strings"
	"testing"
)

func TestNormalizeRepo(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Basic URL normalization
		{
			name:     "HTTPS URL with .git suffix",
			input:    "https://github.com/user/repo.git",
			expected: "github.com/user/repo",
		},
		{
			name:     "HTTP URL without .git suffix",
			input:    "http://github.com/user/repo",
			expected: "github.com/user/repo",
		},
		{
			name:     "SSH URL format",
			input:    "ssh://github.com/user/repo",
			expected: "github.com/user/repo",
		},

		// SSH git@ format conversion
		{
			name:     "git@ SSH format",
			input:    "git@github.com:user/repo.git",
			expected: "github.com/user/repo",
		},
		{
			name:     "git@ SSH format without .git",
			input:    "git@github.com:user/repo",
			expected: "github.com/user/repo",
		},
		{
			name:     "git@ with different host",
			input:    "git@gitlab.com:user/repo.git",
			expected: "gitlab.com/user/repo",
		},

		// Trailing slash handling
		{
			name:     "URL with trailing slash",
			input:    "https://github.com/user/repo/",
			expected: "github.com/user/repo",
		},
		{
			name:     "Multiple trailing slashes",
			input:    "https://github.com/user/repo///",
			expected: "github.com/user/repo//", // Only removes one trailing slash
		},

		// Case normalization
		{
			name:     "Mixed case URL",
			input:    "https://GitHub.com/User/Repo",
			expected: "github.com/user/repo",
		},
		{
			name:     "Upper case git@ format",
			input:    "git@GITHUB.COM:USER/REPO.git",
			expected: "github.com/user/repo",
		},

		// Whitespace handling
		{
			name:     "Leading whitespace",
			input:    "  https://github.com/user/repo",
			expected: "github.com/user/repo",
		},
		{
			name:     "Trailing whitespace",
			input:    "https://github.com/user/repo  ",
			expected: "github.com/user/repo",
		},
		{
			name:     "Leading and trailing whitespace",
			input:    "  https://github.com/user/repo  ",
			expected: "github.com/user/repo",
		},

		// Already normalized URLs
		{
			name:     "Already normalized",
			input:    "github.com/user/repo",
			expected: "github.com/user/repo",
		},
		{
			name:     "Already normalized with uppercase",
			input:    "GitHub.com/User/Repo",
			expected: "github.com/user/repo",
		},

		// Edge cases
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Only whitespace",
			input:    "   ",
			expected: "",
		},
		{
			name:     "Just domain",
			input:    "github.com",
			expected: "github.com",
		},
		{
			name:     "Multiple protocols (shouldn't happen but testing behavior)",
			input:    "https://http://github.com/user/repo",
			expected: "github.com/user/repo",
		},
		{
			name:     "git@ with multiple colons",
			input:    "git@github.com:user:repo:extra",
			expected: "github.com/user:repo:extra", // Only first colon is replaced
		},
		{
			name:     ".git in middle of path",
			input:    "https://github.com/user/repo.git-tools",
			expected: "github.com/user/repo.git-tools", // .git only removed from end
		},
		{
			name:     "Multiple .git suffixes",
			input:    "https://github.com/user/repo.git.git",
			expected: "github.com/user/repo.git", // Only removes one .git suffix
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeRepo(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeRepo(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// Benchmark to test performance with various input sizes
func BenchmarkNormalizeRepo(b *testing.B) {
	inputs := []string{
		"https://github.com/user/repo.git",
		"git@github.com:user/repo.git",
		"  http://github.com/user/repo/  ",
		"SSH://GitHub.com/User/Repo.git/",
		"github.com/user/repo",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, input := range inputs {
			_ = normalizeRepo(input)
		}
	}
}

// Tests for the new function
func TestSameRepoWithOrg(t *testing.T) {
	tests := []struct {
		name         string
		orgName      string
		slug         string
		fullRepoPath string
		expected     bool
	}{
		{
			name:         "Exact match with full path",
			orgName:      "myorg",
			slug:         "myrepo",
			fullRepoPath: "github.com/myorg/myrepo",
			expected:     true,
		},
		{
			name:         "Exact match without domain",
			orgName:      "myorg",
			slug:         "myrepo",
			fullRepoPath: "myorg/myrepo",
			expected:     true,
		},
		{
			name:         "Different organization",
			orgName:      "myorg",
			slug:         "myrepo",
			fullRepoPath: "github.com/differentorg/myrepo",
			expected:     false,
		},
		{
			name:         "Different repository",
			orgName:      "myorg",
			slug:         "myrepo",
			fullRepoPath: "github.com/myorg/differentrepo",
			expected:     false,
		},
		{
			name:         "Both different",
			orgName:      "myorg",
			slug:         "myrepo",
			fullRepoPath: "github.com/differentorg/differentrepo",
			expected:     false,
		},
		{
			name:         "Case sensitive check",
			orgName:      "MyOrg",
			slug:         "MyRepo",
			fullRepoPath: "github.com/myorg/myrepo",
			expected:     false, // Currently case sensitive
		},
		{
			name:         "With trailing slash",
			orgName:      "myorg",
			slug:         "myrepo",
			fullRepoPath: "github.com/myorg/myrepo/",
			expected:     true,
		},
		{
			name:         "Different domain same org/repo",
			orgName:      "myorg",
			slug:         "myrepo",
			fullRepoPath: "gitlab.com/myorg/myrepo",
			expected:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sameRepo(tt.orgName, tt.slug, tt.fullRepoPath)
			if result != tt.expected {
				t.Errorf("sameRepoWithOrg(%q, %q, %q) = %v, want %v",
					tt.orgName, tt.slug, tt.fullRepoPath, result, tt.expected)
			}
		})
	}
}

// Example of how the code should be updated
func TestUpdatedUsageExample(t *testing.T) {
	// Assuming you have access to both orgName and slug from node.Node
	type Node struct {
		OrgName    string
		Slug       string
		Repository struct {
			URL string
		}
	}

	nodes := []Node{
		{OrgName: "correctorg", Slug: "myproject", Repository: struct{ URL string }{URL: "https://github.com/correctorg/myproject.git"}},
		{OrgName: "wrongorg", Slug: "myproject", Repository: struct{ URL string }{URL: "https://github.com/wrongorg/myproject.git"}},
	}

	targetOrg := "correctorg"
	targetSlug := "myproject"

	t.Run("Find correct repository", func(t *testing.T) {
		found := false
		var foundNode Node

		for _, node := range nodes {
			normalizedURL := normalizeRepo(node.Repository.URL)
			// Updated usage with org name
			if sameRepo(targetOrg, targetSlug, normalizedURL) {
				found = true
				foundNode = node
				break
			}
		}

		if !found {
			t.Error("Should have found the correct repository")
		}

		if foundNode.OrgName != "correctorg" {
			t.Errorf("Found wrong repository: %s/%s", foundNode.OrgName, foundNode.Slug)
		}
	})
}

// Alternative: Update the original sameRepo to handle "org/repo" format
func sameRepoImproved(orgSlug, fullRepoPath string) bool {
	// orgSlug should be in format "org/repo"
	if !strings.Contains(orgSlug, "/") {
		// If no org provided, we can't safely match
		return false
	}

	// Exact match
	if orgSlug == fullRepoPath {
		return true
	}

	// Check if fullRepoPath ends with our org/repo
	return strings.HasSuffix(fullRepoPath, "/"+orgSlug)
}

func TestSameRepoImproved(t *testing.T) {
	tests := []struct {
		name         string
		orgSlug      string // "org/repo" format
		fullRepoPath string
		expected     bool
	}{
		{
			name:         "Match with domain",
			orgSlug:      "myorg/myrepo",
			fullRepoPath: "github.com/myorg/myrepo",
			expected:     true,
		},
		{
			name:         "No match - different org",
			orgSlug:      "myorg/myrepo",
			fullRepoPath: "github.com/otherorg/myrepo",
			expected:     false,
		},
		{
			name:         "No org provided - should not match",
			orgSlug:      "myrepo", // Missing org
			fullRepoPath: "github.com/myorg/myrepo",
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sameRepoImproved(tt.orgSlug, tt.fullRepoPath)
			if result != tt.expected {
				t.Errorf("sameRepoImproved(%q, %q) = %v, want %v",
					tt.orgSlug, tt.fullRepoPath, result, tt.expected)
			}
		})
	}
}
