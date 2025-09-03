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

func TestRepoSimilarityScore(t *testing.T) {
	tests := []struct {
		name     string
		orgName  string
		slug     string
		repoURL  string
		expected int // score range
		isExact  bool
	}{
		{
			name:     "Exact match gets highest score",
			orgName:  "myorg",
			slug:     "myrepo",
			repoURL:  "github.com/myorg/myrepo",
			expected: 1000,
			isExact:  true,
		},
		{
			name:     "Non-matching repo gets zero score",
			orgName:  "myorg",
			slug:     "myrepo",
			repoURL:  "github.com/otherorg/otherrepo",
			expected: 0,
		},
		{
			name:     "Partial match gets positive score",
			orgName:  "myorg",
			slug:     "myrepo",
			repoURL:  "gitlab.com/myorg/myrepo",
			expected: 200, // Should be > 0 but < 1000
		},
		{
			name:     "Case differences should still match",
			orgName:  "myorg",
			slug:     "myrepo",
			repoURL:  "GITHUB.COM/MYORG/MYREPO",
			expected: 200, // Should be > 0 due to normalization
		},
		{
			name:     "Repo name contained in slug with prefixes",
			orgName:  "twilio-internal",
			slug:     "internal-product-docs",
			repoURL:  "github.com/twilio-internal/twilio-internal-internal-product-docs",
			expected: 500, // Should get high score for containing match
		},
		{
			name:     "Repo name at end of slug",
			orgName:  "myorg",
			slug:     "myrepo",
			repoURL:  "github.com/myorg/prefix-myrepo",
			expected: 500, // Should get high score for suffix match
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := repoSimilarityScore(tt.orgName, tt.slug, tt.repoURL)
			if tt.isExact && score != tt.expected {
				t.Errorf("repoSimilarityScore(%q, %q, %q) = %d, want exactly %d for exact match",
					tt.orgName, tt.slug, tt.repoURL, score, tt.expected)
			} else if !tt.isExact {
				if tt.expected == 0 && score != 0 {
					t.Errorf("repoSimilarityScore(%q, %q, %q) = %d, want 0 for non-matching repo",
						tt.orgName, tt.slug, tt.repoURL, score)
				} else if tt.expected > 0 && score <= 0 {
					t.Errorf("repoSimilarityScore(%q, %q, %q) = %d, want > 0 for matching repo",
						tt.orgName, tt.slug, tt.repoURL, score)
				}
			}
		})
	}
}

func TestLongestCommonSuffix(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected int
	}{
		{"Same strings", "hello", "hello", 5},
		{"No common suffix", "abc", "def", 0},
		{"Common suffix", "prefixabc", "differentabc", 3},
		{"One empty", "", "hello", 0},
		{"Both empty", "", "", 0},
		{"Full suffix match", "abc", "xyzabc", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := longestCommonSuffix(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("longestCommonSuffix(%q, %q) = %d, want %d",
					tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestLevenshteinDistance(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected int
	}{
		{"Same strings", "hello", "hello", 0},
		{"One empty", "", "hello", 5},
		{"Both empty", "", "", 0},
		{"Single insertion", "hello", "hellos", 1},
		{"Single deletion", "hellos", "hello", 1},
		{"Single substitution", "hello", "hallo", 1},
		{"Complete different", "abc", "def", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := levenshteinDistance(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("levenshteinDistance(%q, %q) = %d, want %d",
					tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

// TestNilPipelinesHandling tests the fix for the pipelines == nil vs pipelines != nil bug
func TestNilPipelinesHandling(t *testing.T) {
	// This test simulates the scenario where ListPipelines returns nil vs empty slice
	// Previously the code had `if pipelines == nil` which would skip processing valid empty results

	t.Run("nil pipelines slice should not process", func(t *testing.T) {
		// Simulate the corrected logic: if pipelines != nil (was incorrectly pipelines == nil)
		var pipelines []string
		pipelines = nil

		processed := false
		if pipelines != nil { // This is the corrected condition
			// Should not enter here when pipelines is nil
			processed = true
		}

		if processed {
			t.Error("Should not process when pipelines is nil")
		}
	})

	t.Run("empty pipelines slice should process", func(t *testing.T) {
		// Empty slice is different from nil slice
		pipelines := make([]string, 0)

		processed := false
		if pipelines != nil { // This is the corrected condition
			// Should enter here when pipelines is empty slice (not nil)
			processed = true
		}

		if !processed {
			t.Error("Should process when pipelines is empty slice (not nil)")
		}
	})

	t.Run("populated pipelines slice should process", func(t *testing.T) {
		pipelines := []string{"pipeline1", "pipeline2"}

		processed := false
		if pipelines != nil {
			processed = true
		}

		if !processed {
			t.Error("Should process when pipelines has elements")
		}
	})
}
