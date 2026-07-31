package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
)

func main() {
	var showVersion bool
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	var verbose bool
	flag.BoolVar(&verbose, "v", false, "verbose output (log every fetched file)")
	var branches multiFlag
	flag.Var(&branches, "b", "additional branch name to check for (repeatable)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: your-git-is-showing [options] URL DIR\n\n")
		fmt.Fprintf(os.Stderr, "Dump a git repository from a website.\n\n")
		flag.PrintDefaults()
	}

	flag.Parse()
	if showVersion {
		fmt.Println(versionString())
		return
	}
	args := flag.Args()
	if len(args) != 2 {
		flag.Usage()
		os.Exit(2)
	}

	url := args[0]
	directory := args[1]

	if err := os.MkdirAll(directory, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error creating directory: %v\n", err)
		os.Exit(2)
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "`%s` is not a directory\n", directory)
		os.Exit(2)
	}

	absDir, err := filepath.Abs(directory)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	if err := fetchGit(FetchOptions{
		BaseURL:   url,
		Directory: absDir,
		Branches:  branches,
		Verbose:   verbose,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("success: %s\n", absDir)
}

type multiFlag []string

func (m *multiFlag) String() string {
	return strings.Join(*m, ", ")
}

func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

// versionString reports the module version and VCS revision stamped into the
// binary by the build system (e.g. "v0.1.0 (abcd1234)" via `go install`).
func versionString() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	version := info.Main.Version
	if version == "" || version == "(devel)" {
		version = "dev"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 8 {
			return fmt.Sprintf("%s (%s)", version, s.Value[:8])
		}
	}
	return version
}
