package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var headPattern = regexp.MustCompile(`^(ref:.*|[0-9a-f]{40}$)`)

var branchPattern = regexp.MustCompile(`^[A-Za-z0-9\-\._]+$`)

var packRe = regexp.MustCompile(`pack-([a-f0-9]{40})\.pack`)

var defaultRefTasks = []string{
	".git/FETCH_HEAD",
	".git/HEAD",
	".git/ORIG_HEAD",
	".git/config",
	".git/info/refs",
	".git/logs/HEAD",
	".git/logs/refs/remotes/origin/HEAD",
	".git/logs/refs/stash",
	".git/packed-refs",
	".git/refs/remotes/origin/HEAD",
	".git/refs/stash",
}

var defaultBranches = []string{
	"main",
	"master",
	"staging",
	"production",
	"development",
	"dev",
	"develop",
	"release",
	"qa",
	"hotfix",
}

func branchRefTasks(branches []string) []string {
	var tasks []string
	for _, branch := range branches {
		tasks = append(tasks,
			fmt.Sprintf(".git/logs/refs/heads/%s", branch),
			fmt.Sprintf(".git/refs/heads/%s", branch),
			fmt.Sprintf(".git/logs/refs/remotes/origin/%s", branch),
			fmt.Sprintf(".git/refs/remotes/origin/%s", branch),
			fmt.Sprintf(".git/refs/wip/wtree/refs/heads/%s", branch),
			fmt.Sprintf(".git/refs/wip/index/refs/heads/%s", branch),
		)
	}
	return tasks
}

var commonFileTasks = []string{
	".gitignore",
	".git/COMMIT_EDITMSG",
	".git/description",
	".git/hooks/applypatch-msg.sample",
	".git/hooks/commit-msg.sample",
	".git/hooks/post-commit.sample",
	".git/hooks/post-receive.sample",
	".git/hooks/post-update.sample",
	".git/hooks/pre-applypatch.sample",
	".git/hooks/pre-commit.sample",
	".git/hooks/pre-push.sample",
	".git/hooks/pre-rebase.sample",
	".git/hooks/pre-receive.sample",
	".git/hooks/prepare-commit-msg.sample",
	".git/hooks/update.sample",
	".git/index",
	".git/info/exclude",
	".git/objects/info/packs",
}

func normalizeBaseURL(raw string) string {
	url := strings.TrimRight(raw, "/")
	if strings.HasSuffix(url, "HEAD") {
		url = strings.TrimRight(url[:len(url)-4], "/")
	}
	if strings.HasSuffix(url, ".git") {
		url = strings.TrimRight(url[:len(url)-4], "/")
	}
	return url
}

type FetchOptions struct {
	BaseURL   string
	Directory string
	Branches  []string
	Verbose   bool
}

func fetchGit(opts FetchOptions) error {
	if info, err := os.Stat(opts.Directory); err != nil || !info.IsDir() {
		return fmt.Errorf("%s is not a directory", opts.Directory)
	}

	if entries, _ := os.ReadDir(opts.Directory); len(entries) > 0 {
		fmt.Fprintf(os.Stderr, "Warning: Destination '%s' is not empty\n", opts.Directory)
	}

	ctx := &downloadContext{
		baseURL:   normalizeBaseURL(opts.BaseURL),
		directory: opts.Directory,
		client:    newHTTPClient(),
		verbose:   opts.Verbose,
	}

	listing, err := probe(ctx)
	if err != nil {
		return err
	}
	if listing {
		if err := fetchRecursive(ctx); err != nil {
			return err
		}
	} else if err := fetchTargeted(ctx, opts); err != nil {
		return err
	}
	ctx.logf("[+] Done. %d files fetched.\n", ctx.fetched.Load())
	return nil
}

// probe verifies the target serves a usable .git directory and reports
// whether /.git/ exposes an HTML directory listing.
func probe(ctx *downloadContext) (bool, error) {
	url := ctx.baseURL

	ctx.logf("[-] Testing %s/.git/HEAD ", url)
	resp, body, err := ctx.doGet(".git/HEAD")
	if err != nil {
		return false, fmt.Errorf("unable to connect to %s: %w", url, err)
	}
	ctx.logf("[%d]\n", resp.StatusCode)
	if err := verifyResponse(resp); err != nil {
		return false, fmt.Errorf("%s/.git/HEAD %w", url, err)
	}
	if !headPattern.MatchString(strings.TrimSpace(string(body))) {
		return false, fmt.Errorf("%s/.git/HEAD is not a git HEAD file", url)
	}

	ctx.logf("[-] Testing %s/.git/ ", url)
	resp, body, err = ctx.doGet(".git/")
	if err != nil {
		return false, fmt.Errorf("error fetching .git/: %w", err)
	}
	ctx.logf("[%d]\n", resp.StatusCode)

	if resp.StatusCode != http.StatusOK || !isHTML(resp) {
		return false, nil
	}
	for _, f := range getIndexedFiles(string(body)) {
		if f == "HEAD" {
			return true, nil
		}
	}
	return false, nil
}

// fetchRecursive downloads the entire .git tree from an HTML directory listing.
func fetchRecursive(ctx *downloadContext) error {
	ctx.logf("[-] Fetching .git recursively\n")
	if err := processTasks([]string{".git/", ".gitignore"}, defaultJobs, makeRecursiveDownloadWorker(ctx), nil); err != nil {
		return err
	}

	ctx.logf("[-] Sanitizing .git/config\n")
	if err := sanitizeFile(filepath.Join(ctx.directory, ".git", "config")); err != nil {
		return err
	}
	ctx.logf("[-] Running git checkout .\n")
	if err := runGitCheckout(ctx.directory); err != nil {
		return fmt.Errorf("git checkout failed: %w", err)
	}
	return nil
}

// fetchTargeted reconstructs the repo without a directory listing by
// fetching known files, refs, packs, and loose objects.
func fetchTargeted(ctx *downloadContext, opts FetchOptions) error {
	ctx.logf("[-] Fetching common files\n")
	if err := processTasks(commonFileTasks, defaultJobs, makeDownloadWorker(ctx), nil); err != nil {
		return err
	}

	ctx.logf("[-] Finding refs/\n")
	refTasks := append([]string{}, defaultRefTasks...)
	refTasks = append(refTasks, branchRefTasks(defaultBranches)...)
	for _, branch := range opts.Branches {
		if !branchPattern.MatchString(branch) {
			fmt.Fprintf(os.Stderr, "Warning: ignoring invalid branch name '%s'\n", branch)
			continue
		}
		refTasks = append(refTasks, branchRefTasks([]string{branch})...)
	}
	if err := processTasks(refTasks, defaultJobs, makeFindRefsWorker(ctx), nil); err != nil {
		return err
	}

	ctx.logf("[-] Finding packs\n")
	if err := processTasks(packTasksFromInfo(ctx.directory), defaultJobs, makeDownloadWorker(ctx), nil); err != nil {
		return err
	}

	ctx.logf("[-] Finding objects\n")
	objs, packedObjs, err := collectObjectSHAs(ctx.directory)
	if err != nil {
		return fmt.Errorf("error collecting objects: %w", err)
	}

	objList := make([]string, 0, len(objs))
	for obj := range objs {
		objList = append(objList, obj)
	}

	ctx.logf("[-] Fetching objects\n")
	if err := processTasks(objList, defaultJobs, makeFindObjectsWorker(ctx), packedObjs); err != nil {
		return err
	}

	ctx.logf("[-] Sanitizing .git/config\n")
	if err := sanitizeFile(filepath.Join(ctx.directory, ".git", "config")); err != nil {
		return err
	}
	ctx.logf("[-] Running git checkout .\n")
	if err := runGitCheckout(ctx.directory); err != nil {
		return fmt.Errorf("git checkout failed: %w", err)
	}
	return nil
}

// packTasksFromInfo lists the .idx/.pack pairs named in .git/objects/info/packs.
func packTasksFromInfo(directory string) []string {
	var packTasks []string
	infoPacksPath := filepath.Join(directory, ".git", "objects", "info", "packs")
	if data, err := os.ReadFile(infoPacksPath); err == nil {
		for _, m := range packRe.FindAllStringSubmatch(string(data), -1) {
			sha1 := m[1]
			packTasks = append(packTasks,
				fmt.Sprintf(".git/objects/pack/pack-%s.idx", sha1),
				fmt.Sprintf(".git/objects/pack/pack-%s.pack", sha1),
			)
		}
	}
	return packTasks
}

func runGitCheckout(directory string) error {
	cmd := exec.Command("git", "-C", directory, "checkout", ".")
	return cmd.Run()
}
