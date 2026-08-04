package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var unsafeConfigLine = regexp.MustCompile(`(?im)^\s*(fsmonitor|sshcommand|askpass|editor|pager)`)

// isSafePath reports whether path, joined to baseDir, stays inside baseDir.
// baseDir is resolved through symlinks first so checks survive dump
// directories that live behind one (e.g. macOS /var -> /private/var); the
// joined path is likewise resolved when it already exists.
func isSafePath(baseDir, path string) bool {
	if strings.HasPrefix(path, "/") {
		return false
	}
	base, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		base = filepath.Clean(baseDir)
	}
	joined := filepath.Join(base, path)
	real, err := filepath.EvalSymlinks(joined)
	if err != nil {
		real = filepath.Clean(joined)
	}
	return isWithinDir(real, base)
}

func isWithinDir(path, dir string) bool {
	if path == dir {
		return true
	}
	if !strings.HasPrefix(path, dir) {
		return false
	}
	return len(path) > len(dir) && path[len(dir)] == filepath.Separator
}

func createIntermediateDirs(path string) {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
}

func sanitizeFile(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	modified := unsafeConfigLine.ReplaceAllStringFunc(string(content), func(s string) string {
		return "# " + s
	})
	if string(content) != modified {
		fmt.Printf("Warning: '%s' file was altered\n", path)
		if err := os.WriteFile(path, []byte(modified), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	return nil
}
