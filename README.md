# your-git-is-showing

## NAME

your-git-is-showing — dump an exposed git repository

## SYNOPSIS

```
your-git-is-showing [options] URL DIR
```

## DESCRIPTION

Your `.git` is showing! `your-git-is-showing` is a single-purpose tool that downloads an exposed Git repository served over HTTP. Given a site URL, it fetches `.git` metadata (HEAD, refs, index, pack files, loose objects) and reconstructs the repository locally. Enumerate a site to find the `.git`, or fire-and-forget and see what comes back.

`URL` is the site root (or the directory containing the repo); the tool appends `/.git` paths for you. `DIR` is the local directory where the reconstructed repo is written. On success the tool sanitizes `.git/config` (commenting out `fsmonitor`, `sshCommand`, `askpass`, `editor`, `pager` entries) and runs `git checkout .` to restore the working tree.

If `/.git/` serves an HTML directory listing, the tool switches to recursive mode and walks the listing; otherwise it uses targeted fetching of common files, refs, pack files, and loose objects.

## FEATURES

- 50 concurrent requests
- 3 retries per request, 3s per-request timeout
- Browser user-agent by default
- Probes 10 common branch names by default: `main`, `master`, `staging`, `production`, `development`, `dev`, `develop`, `release`, `qa`, `hotfix`
- Recursive download of `/.git/` when the server exposes a directory listing

## OPTIONS

| Flag | Description |
| --- | --- |
| `-b string` | additional branch name to check for (repeatable) |
| `-version` | print version and exit |

## INSTALL

```
go install github.com/meta-byte/your-git-is-showing@latest
```

Requires Go 1.25+. Installs into `$GOPATH/bin`. Or build from source:

```
go build .
```

## EXAMPLES

```
your-git-is-showing https://example.com/ myrepo
your-git-is-showing -b develop https://example.com/repo/ repo-dump
```

## EXIT STATUS

| Code | Meaning |
| --- | --- |
| 0 | success — somebody's secrets just became yours |
| 1 | dump failed |
| 2 | invalid usage |

## BUILD

```
go build .
go test ./...
```

## DEPENDENCIES

`golang.org/x/net` (HTML parsing), `golang.org/x/sync` (bounded worker pool).

## DISCLAIMER

Fun, but not a toy. Use this tool solely against systems you own or have explicit permission to test. The author assumes no liability for misuse. Happy hacking.
