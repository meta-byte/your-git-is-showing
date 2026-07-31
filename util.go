package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var unsafeConfigLine = regexp.MustCompile(`(?im)^\s*(fsmonitor|sshcommand|askpass|editor|pager)`)

func isSafePath(path string) bool {
	if strings.HasPrefix(path, "/") {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	joined := filepath.Join(home, path)
	real, err := filepath.EvalSymlinks(joined)
	if err != nil {
		real = filepath.Clean(joined)
	}
	homeReal, err := filepath.EvalSymlinks(home)
	if err != nil {
		homeReal = filepath.Clean(home)
	}
	return isWithinDir(real, homeReal)
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
