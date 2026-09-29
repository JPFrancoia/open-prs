# Open PRs

Open PRs lists open GitHub pull requests and GitLab merge requests from the repositories in `REPOS`.

## Prerequisites

Install `gh` and `glab`. Authenticate both commands for the repositories that you use.

## Build

Run this command:

```sh
make build
```

## Run

Set `REPOS` to a comma-separated list of GitHub and GitLab repository URLs. Then run the program:

```sh
REPOS=https://github.com/OWNER/REPOSITORY,https://gitlab.com/GROUP/PROJECT ./open-prs
```

The program fetches all repositories concurrently and prints them in `REPOS` order. It shows each request's title, age since creation, approval status, and author in square brackets. In a terminal, titles are blue clickable links, ages are yellow, and authors are magenta. `NO_COLOR` removes colors but keeps clickable titles. When piped or used with `TERM=dumb`, it prints Markdown links with their URLs. It prints at most 100 open requests for each repository.

If one repository fails, the program prints the error to standard error. It continues with the other repositories and exits with status 1.
