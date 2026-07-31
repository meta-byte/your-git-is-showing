package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/sync/errgroup"
)

const defaultJobs = 50

// processTasks runs a bounded worker pool until all tasks complete.
// worker returns newly discovered tasks; the first error cancels the rest.
func processTasks(initialTasks []string, jobs int, worker func(string) ([]string, error), preDone map[string]struct{}) error {
	if len(initialTasks) == 0 {
		return nil
	}

	g, ctx := errgroup.WithContext(context.Background())
	g.SetLimit(jobs)

	seen := make(map[string]struct{})
	for k := range preDone {
		seen[k] = struct{}{}
	}

	pendingTasks := make(chan string, jobs)
	doneCh := make(chan []string, jobs)
	numPending := 0

	enqueue := func(task string) bool {
		select {
		case <-ctx.Done():
			return false
		case pendingTasks <- task:
			numPending++
			seen[task] = struct{}{}
			return true
		}
	}

	for _, task := range initialTasks {
		if _, ok := seen[task]; ok {
			continue
		}
		if !enqueue(task) {
			break
		}
	}

	for range jobs {
		g.Go(func() error {
			for {
				select {
				case <-ctx.Done():
					return nil
				case task, ok := <-pendingTasks:
					if !ok {
						return nil
					}
					refs, err := worker(task)
					if err != nil {
						return err
					}
					select {
					case <-ctx.Done():
						return nil
					case doneCh <- refs:
					}
				}
			}
		})
	}

drain:
	for numPending > 0 {
		select {
		case <-ctx.Done():
			break drain
		case refs := <-doneCh:
			numPending--
			for _, task := range refs {
				if _, ok := seen[task]; ok {
					continue
				}
				if !enqueue(task) {
					break drain
				}
			}
		}
	}

	close(pendingTasks)
	return g.Wait()
}

func fetchToFile(ctx *downloadContext, relPath string) (string, error) {
	localPath := filepath.Join(ctx.directory, relPath)
	if _, err := os.Stat(localPath); err == nil {
		if ctx.verbose {
			fmt.Printf("[-] Already downloaded %s/%s\n", ctx.baseURL, relPath)
		}
		absPath, _ := filepath.Abs(localPath)
		return absPath, nil
	}

	resp, err := ctx.doGetStream(relPath)
	if err != nil {
		return "", fmt.Errorf("fetching %s/%s: %w", ctx.baseURL, relPath, err)
	}
	defer resp.Body.Close()

	if ctx.verbose {
		fmt.Printf("[-] Fetching %s/%s [%d]\n", ctx.baseURL, relPath, resp.StatusCode)
	}

	if !downloadOK(ctx, resp, relPath) {
		return "", nil
	}

	absPath, _ := filepath.Abs(localPath)
	createIntermediateDirs(absPath)
	if err := writeFile(resp.Body, absPath); err != nil {
		return "", err
	}
	ctx.fetched.Add(1)
	return absPath, nil
}

// downloadOK reports whether a response is a valid file download. Soft
// failures (wrong status, empty body, HTML) are skipped and only reported
// in verbose mode.
func downloadOK(ctx *downloadContext, resp *http.Response, relPath string) bool {
	if resp.StatusCode != http.StatusOK {
		if ctx.verbose {
			fmt.Fprintf(os.Stderr, "[-] %s/%s responded with status code %d\n", ctx.baseURL, relPath, resp.StatusCode)
		}
		return false
	}
	if resp.Header.Get("Content-Length") == "0" {
		if ctx.verbose {
			fmt.Fprintf(os.Stderr, "[-] %s/%s responded with a zero-length body\n", ctx.baseURL, relPath)
		}
		return false
	}
	if isHTML(resp) {
		if ctx.verbose {
			fmt.Fprintf(os.Stderr, "[-] %s/%s responded with HTML\n", ctx.baseURL, relPath)
		}
		return false
	}
	return true
}

func writeFile(r io.Reader, absPath string) error {
	f, err := os.Create(absPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", absPath, err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return fmt.Errorf("writing %s: %w", absPath, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", absPath, err)
	}
	return nil
}

func makeDownloadWorker(ctx *downloadContext) func(string) ([]string, error) {
	return func(relPath string) ([]string, error) {
		_, err := fetchToFile(ctx, relPath)
		return nil, err
	}
}

func makeRecursiveDownloadWorker(ctx *downloadContext) func(string) ([]string, error) {
	return func(relPath string) ([]string, error) {
		localPath := filepath.Join(ctx.directory, relPath)
		if info, err := os.Stat(localPath); err == nil && !info.IsDir() {
			if ctx.verbose {
				fmt.Printf("[-] Already downloaded %s/%s\n", ctx.baseURL, relPath)
			}
			return nil, nil
		}

		resp, err := ctx.doGetStream(relPath)
		if err != nil {
			return nil, fmt.Errorf("fetching %s/%s: %w", ctx.baseURL, relPath, err)
		}
		defer resp.Body.Close()

		if ctx.verbose {
			fmt.Printf("[-] Fetching %s/%s [%d]\n", ctx.baseURL, relPath, resp.StatusCode)
		}

		if (resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusFound) &&
			resp.Header.Get("Location") != "" &&
			strings.HasSuffix(resp.Header.Get("Location"), relPath+"/") {
			return []string{relPath + "/"}, nil
		}

		if strings.HasSuffix(relPath, "/") {
			return parseDirectoryListing(resp, relPath)
		}

		if !downloadOK(ctx, resp, relPath) {
			return nil, nil
		}

		absPath, _ := filepath.Abs(localPath)
		createIntermediateDirs(absPath)
		if err := writeFile(resp.Body, absPath); err != nil {
			return nil, err
		}
		ctx.fetched.Add(1)
		return nil, nil
	}
}

func parseDirectoryListing(resp *http.Response, relPath string) ([]string, error) {
	if !isHTML(resp) {
		return nil, nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading directory listing %s: %w", relPath, err)
	}
	var tasks []string
	for _, name := range getIndexedFiles(string(body)) {
		tasks = append(tasks, relPath+name)
	}
	return tasks, nil
}

var refPattern = regexp.MustCompile(`(refs(/[a-zA-Z0-9\-\._*]+)+)`)

func makeFindRefsWorker(ctx *downloadContext) func(string) ([]string, error) {
	return func(relPath string) ([]string, error) {
		absPath, err := fetchToFile(ctx, relPath)
		if err != nil {
			return nil, err
		}
		if absPath == "" {
			return nil, nil
		}

		body, err := os.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", absPath, err)
		}

		var tasks []string
		for _, m := range refPattern.FindAllStringSubmatch(string(body), -1) {
			ref := m[1]
			if strings.HasSuffix(ref, "*") {
				continue
			}
			if !isSafePath(ref) {
				continue
			}
			tasks = append(tasks, ".git/"+ref, ".git/logs/"+ref)
		}
		return tasks, nil
	}
}

func makeFindObjectsWorker(ctx *downloadContext) func(string) ([]string, error) {
	return func(obj string) ([]string, error) {
		relPath := fmt.Sprintf(".git/objects/%s/%s", obj[:2], obj[2:])
		absPath, err := fetchToFile(ctx, relPath)
		if err != nil {
			return nil, err
		}
		if absPath == "" {
			return nil, nil
		}
		return getReferencedSHA1(absPath)
	}
}
