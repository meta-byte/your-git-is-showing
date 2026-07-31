package main

import "testing"

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
