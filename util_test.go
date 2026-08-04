package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsWithinDir(t *testing.T) {
	tests := []struct {
		name string
		path string
		dir  string
		want bool
	}{
		{"same", "/a", "/a", true},
		{"direct child", "/a/b", "/a", true},
		{"nested", "/a/b/c", "/a", true},
		{"sibling prefix", "/ab", "/a", false},
		{"outside", "/b/c", "/a", false},
		{"dir with trailing slash", "/a/", "/a", true},
		{"parent", "/a", "/a/b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isWithinDir(tt.path, tt.dir); got != tt.want {
				t.Errorf("isWithinDir(%q, %q) = %v, want %v", tt.path, tt.dir, got, tt.want)
			}
		})
	}
}

func TestIsSafePath(t *testing.T) {
	base := t.TempDir()
	_ = os.MkdirAll(filepath.Join(base, ".git", "objects"), 0o755)

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"empty", "", true},
		{"nested inside", ".git/objects/ab/cd", true},
		{"inside with dots", ".git/objects/../refs", true},
		{"absolute", "/etc/passwd", false},
		{"single dotdot", "..", false},
		{"escaping", "../../etc/passwd", false},
		{"escaping deep", "a/../../../etc/passwd", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSafePath(base, tt.path); got != tt.want {
				t.Errorf("isSafePath(%q, %q) = %v, want %v", base, tt.path, got, tt.want)
			}
		})
	}
}
