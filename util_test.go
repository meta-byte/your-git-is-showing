package main

import "testing"

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
