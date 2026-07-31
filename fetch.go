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
}

func fetchGit(opts FetchOptions) error {
	if info, err := os.Stat(opts.Directory); err != nil || !info.IsDir() {
		return fmt.Errorf("%s is not a directory", opts.Directory)
	}

	client := newHTTPClient()

	if entries, _ := os.ReadDir(opts.Directory); len(entries) > 0 {
		fmt.Printf("Warning: Destination '%s' is not empty\n", opts.Directory)
	}

	url := normalizeBaseURL(opts.BaseURL)
	ctx := &downloadContext{
		baseURL:   url,
		directory: opts.Directory,
		client:    client,
	}

	fmt.Printf("[-] Testing %s/.git/HEAD ", url)
	resp, body, err := ctx.doGet(".git/HEAD")
	if err != nil {
		return fmt.Errorf("unable to connect to %s: %w", url, err)
	}
	fmt.Printf("[%d]\n", resp.StatusCode)
	if err := verifyResponse(resp); err != nil {
		return fmt.Errorf("%s/.git/HEAD %w", url, err)
	}
	if !headPattern.MatchString(strings.TrimSpace(string(body))) {
		return fmt.Errorf("%s/.git/HEAD is not a git HEAD file", url)
	}

	fmt.Printf("[-] Testing %s/.git/ ", url)
	gitDirResp, gitDirBody, err := ctx.doGet(".git/")
	if err != nil {
		return fmt.Errorf("error fetching .git/: %w", err)
	}
	fmt.Printf("[%d]\n", gitDirResp.StatusCode)

	if gitDirResp.StatusCode == http.StatusOK && isHTML(gitDirResp) {
		indexed := getIndexedFiles(string(gitDirBody))
		hasHead := false
		for _, f := range indexed {
			if f == "HEAD" {
				hasHead = true
				break
			}
		}
		if hasHead {
			fmt.Printf("[-] Fetching .git recursively\n")
			if err := processTasks([]string{".git/", ".gitignore"}, defaultJobs, makeRecursiveDownloadWorker(ctx), nil); err != nil {
				return err
			}

			fmt.Printf("[-] Sanitizing .git/config\n")
			if err := sanitizeFile(filepath.Join(opts.Directory, ".git", "config")); err != nil {
				return err
			}
			fmt.Printf("[-] Running git checkout .\n")
			if err := runGitCheckout(opts.Directory); err != nil {
				return fmt.Errorf("git checkout failed: %w", err)
			}
			return nil
		}
	}

	fmt.Printf("[-] Fetching common files\n")
	if err := processTasks(commonFileTasks, defaultJobs, makeDownloadWorker(ctx), nil); err != nil {
		return err
	}

	fmt.Printf("[-] Finding refs/\n")
	refTasks := append([]string{}, defaultRefTasks...)
	refTasks = append(refTasks, branchRefTasks(defaultBranches)...)
	for _, branch := range opts.Branches {
		if !branchPattern.MatchString(branch) {
			fmt.Printf("Warning: ignoring invalid branch name '%s'\n", branch)
			continue
		}
		refTasks = append(refTasks, branchRefTasks([]string{branch})...)
	}
	if err := processTasks(refTasks, defaultJobs, makeFindRefsWorker(ctx), nil); err != nil {
		return err
	}

	fmt.Printf("[-] Finding packs\n")
	var packTasks []string
	infoPacksPath := filepath.Join(opts.Directory, ".git", "objects", "info", "packs")
	if data, err := os.ReadFile(infoPacksPath); err == nil {
		for _, m := range packRe.FindAllStringSubmatch(string(data), -1) {
			sha1 := m[1]
			packTasks = append(packTasks,
				fmt.Sprintf(".git/objects/pack/pack-%s.idx", sha1),
				fmt.Sprintf(".git/objects/pack/pack-%s.pack", sha1),
			)
		}
	}
	if err := processTasks(packTasks, defaultJobs, makeDownloadWorker(ctx), nil); err != nil {
		return err
	}

	fmt.Printf("[-] Finding objects\n")
	objs, packedObjs, err := collectObjectSHAs(opts.Directory)
	if err != nil {
		return fmt.Errorf("error collecting objects: %w", err)
	}

	objList := make([]string, 0, len(objs))
	for obj := range objs {
		objList = append(objList, obj)
	}

	fmt.Printf("[-] Fetching objects\n")
	if err := processTasks(objList, defaultJobs, makeFindObjectsWorker(ctx), packedObjs); err != nil {
		return err
	}

	fmt.Printf("[-] Running git checkout .\n")
	if err := sanitizeFile(filepath.Join(opts.Directory, ".git", "config")); err != nil {
		return err
	}

	if err := runGitCheckout(opts.Directory); err != nil {
		return fmt.Errorf("git checkout failed: %w", err)
	}

	return nil
}

func runGitCheckout(directory string) error {
	cmd := exec.Command("git", "-C", directory, "checkout", ".")
	return cmd.Run()
}
