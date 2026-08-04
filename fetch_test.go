package main

import "testing"

func TestBranchPattern(t *testing.T) {
	tests := []struct {
		name   string
		branch string
		want   bool
	}{
		{"plain", "main", true},
		{"namespaced", "feature/login-flow", true},
		{"deep namespace", "team/feature/export", true},
		{"digits and dots", "release-1.2.0", true},
		{"underscores", "my_branch", true},
		{"hyphen start", "-foo", false},
		{"dot start", ".foo", false},
		{"space", "foo bar", false},
		{"traversal", "../etc", false},
		{"at-sign", "foo@{0}", false},
		{"backslash", "foo\\bar", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := branchPattern.MatchString(tt.branch); got != tt.want {
				t.Errorf("branchPattern.MatchString(%q) = %v, want %v", tt.branch, got, tt.want)
			}
		})
	}
}

func TestIsSafeBranchName(t *testing.T) {
	tests := []struct {
		name   string
		branch string
		want   bool
	}{
		{"plain", "main", true},
		{"namespaced", "feature/login-flow", true},
		{"empty component", "a//b", false},
		{"dot component", "a/./b", false},
		{"dotdot component", "a/../b", false},
		{"pure dotdot", "..", false},
		{"pure dot", ".", false},
		{"trailing slash", "a/", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSafeBranchName(tt.branch); got != tt.want {
				t.Errorf("isSafeBranchName(%q) = %v, want %v", tt.branch, got, tt.want)
			}
		})
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "https://example.com/repo", "https://example.com/repo"},
		{"trailing slash", "https://example.com/repo/", "https://example.com/repo"},
		{"trailing git", "https://example.com/repo/.git", "https://example.com/repo"},
		{"trailing HEAD", "https://example.com/repo/HEAD", "https://example.com/repo"},
		{"git and HEAD", "https://example.com/repo/.git/HEAD", "https://example.com/repo"},
		{"git and slash", "https://example.com/repo/.git/", "https://example.com/repo"},
		{"root", "https://example.com", "https://example.com"},
		{"path ending in HEAD suffix", "https://example.com/repoHEAD/", "https://example.com/repo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeBaseURL(tt.in); got != tt.want {
				t.Errorf("normalizeBaseURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
