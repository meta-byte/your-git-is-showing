package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	var branches multiFlag
	flag.Var(&branches, "b", "additional branch name to check for (repeatable)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: your-git-is-showing [options] URL DIR\n\n")
		fmt.Fprintf(os.Stderr, "Dump a git repository from a website.\n\n")
		flag.PrintDefaults()
	}

	flag.Parse()
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
	}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

type multiFlag []string

func (m *multiFlag) String() string {
	return strings.Join(*m, ", ")
}

func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}
