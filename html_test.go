package main

import "testing"

func TestGetIndexedFiles(t *testing.T) {
	base := t.TempDir()
	body := `<html><body>
		<a href="HEAD">HEAD</a>
		<a href="objects/">objects</a>
		<a href="/etc/passwd">abs</a>
		<a href="../../etc/passwd">escape</a>
		<a href="http://evil.com/x">external</a>
	</body></html>`
	want := []string{"HEAD", "objects/"}
	got := getIndexedFiles(base, body)
	if !equalStrings(got, want) {
		t.Errorf("getIndexedFiles() = %v, want %v", got, want)
	}
}
