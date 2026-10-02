package openprs

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	ent "open-prs/internal/entities"

	"golang.org/x/term"
)

// githubRequest and gitlabResponse are the response shapes of the forge CLIs.
type githubRequest struct {
	Title          string                 `json:"title"`
	Url            string                 `json:"url"`
	CreatedAt      time.Time              `json:"createdAt"`
	Author         struct{ Login string } `json:"author"`
	ReviewDecision string                 `json:"reviewDecision"`
}

type gitlabResponse struct {
	Data struct {
		Project *struct {
			MergeRequests struct {
				Nodes []struct {
					Title     string    `json:"title"`
					WebUrl    string    `json:"webUrl"`
					CreatedAt time.Time `json:"createdAt"`
					Approved  bool      `json:"approved"`
					Author    *struct {
						Username string `json:"username"`
					} `json:"author"`
				} `json:"nodes"`
			} `json:"mergeRequests"`
		} `json:"project"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// fetchResult holds one repository's request list or fetch error.
type fetchResult struct {
	requests []ent.Request
	err      error
}

// parseRepos reads the comma-separated REPOS list in its configured order.
func parseRepos(value string) ([]ent.Repo, []string) {
	var repos []ent.Repo
	var errors []string
	for _, raw := range strings.Split(value, ",") {
		entry := strings.TrimRight(strings.TrimSpace(raw), "/")
		if entry == "" {
			continue
		}
		address := entry
		if !strings.Contains(address, "://") {
			address = "https://" + address
		}
		u, err := url.Parse(address)
		if err != nil || u.Hostname() == "" {
			errors = append(errors, fmt.Sprintf("Not a URL: %s", entry))
			continue
		}
		path := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
		host := strings.ToLower(u.Hostname())
		forge := ""
		switch {
		case strings.HasPrefix(host, "gitlab"):
			forge = "gitlab"
		case strings.HasPrefix(host, "github"):
			forge = "github"
		}
		if forge == "" {
			errors = append(errors, fmt.Sprintf("Unsupported host (expected gitlab* or github*): %s", entry))
		} else if !strings.Contains(path, "/") {
			errors = append(errors, fmt.Sprintf("Not a repository path: %s", entry))
		} else {
			repos = append(repos, ent.Repo{Host: host, Path: path, Forge: forge})
		}
	}
	return repos, errors
}

// fetch retrieves up to 100 open requests with approval information.
func fetch(ctx context.Context, repo ent.Repo) ([]ent.Request, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var bin string
	var args []string
	if repo.Forge == "gitlab" {
		path, err := json.Marshal(repo.Path)
		if err != nil {
			return nil, fmt.Errorf("%s: quote repository path: %w", repo.Path, err)
		}
		bin = "glab"
		args = []string{"api", "--hostname", repo.Host, "graphql", "-f", fmt.Sprintf("query={project(fullPath:%s){mergeRequests(state:opened,first:100){nodes{title webUrl createdAt approved author{username}}}}}", path)}
	} else {
		bin = "gh"
		args = []string{"pr", "list", "--repo", repo.Host + "/" + repo.Path, "--state", "open", "--limit", "100", "--json", "title,url,createdAt,author,reviewDecision"}
	}
	output, err := exec.CommandContext(ctx, bin, args...).Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && len(exit.Stderr) > 0 {
			return nil, fmt.Errorf("%s: %s: %w", repo.Path, strings.TrimSpace(string(exit.Stderr)), err)
		}
		return nil, fmt.Errorf("%s: run %s: %w", repo.Path, bin, err)
	}
	var requests []ent.Request
	if repo.Forge == "gitlab" {
		var response gitlabResponse
		if err := json.Unmarshal(output, &response); err != nil {
			return nil, fmt.Errorf("%s: parse glab output: %w", repo.Path, err)
		}
		if len(response.Errors) > 0 {
			return nil, fmt.Errorf("%s: glab: %s", repo.Path, response.Errors[0].Message)
		}
		if response.Data.Project == nil {
			return nil, fmt.Errorf("%s: project not found in glab response", repo.Path)
		}
		for _, item := range response.Data.Project.MergeRequests.Nodes {
			if item.CreatedAt.IsZero() {
				return nil, fmt.Errorf("%s: glab response missing createdAt", repo.Path)
			}
			author := "unknown"
			if item.Author != nil && item.Author.Username != "" {
				author = item.Author.Username
			}
			requests = append(requests, ent.Request{Title: item.Title, Url: item.WebUrl, CreatedAt: item.CreatedAt, Author: author, Approved: item.Approved})
		}
	} else {
		var response []githubRequest
		if err := json.Unmarshal(output, &response); err != nil {
			return nil, fmt.Errorf("%s: parse gh output: %w", repo.Path, err)
		}
		for _, item := range response {
			if item.CreatedAt.IsZero() {
				return nil, fmt.Errorf("%s: gh response missing createdAt", repo.Path)
			}
			author := item.Author.Login
			if author == "" {
				author = "unknown"
			}
			requests = append(requests, ent.Request{Title: item.Title, Url: item.Url, CreatedAt: item.CreatedAt, Author: author, Approved: item.ReviewDecision == "APPROVED"})
		}
	}
	return requests, nil
}

// age describes elapsed time with the same hour and day cutoffs as /open.
func age(createdAt, now time.Time) string {
	hours := int(now.Sub(createdAt).Hours())
	if hours < 1 {
		return "just now"
	}
	if hours < 24 {
		return fmt.Sprintf("%dh ago", hours)
	}
	return fmt.Sprintf("%dd ago", hours/24)
}

// plainText removes terminal controls from forge-provided text.
func plainText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
}

// markdownText escapes forge-provided text before inserting it into Markdown.
func markdownText(value string) string {
	return strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`, `*`, `\*`, `_`, `\_`, "`", "\\`", `~`, `\~`).Replace(html.EscapeString(plainText(value)))
}

// safeUrl accepts only HTTP links and removes Markdown delimiter characters.
func safeUrl(value string) string {
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return ""
	}
	address, err := url.Parse(value)
	if err != nil || (address.Scheme != "https" && address.Scheme != "http") || address.Host == "" {
		return ""
	}
	return strings.NewReplacer("<", "%3C", ">", "%3E").Replace(address.String())
}

// requestLink formats a safe HTTP link or leaves an invalid URL as plain text.
func requestLink(request ent.Request) string {
	title := markdownText(request.Title)
	if address := safeUrl(request.Url); address != "" {
		return fmt.Sprintf("[%s](<%s>)", title, address)
	}
	return title + " (" + markdownText(request.Url) + ")"
}

// Run prints the configured open requests and returns a nonzero status on errors.
func Run(ctx context.Context, value string, out, errs io.Writer) int {
	repos, problems := parseRepos(value)
	if len(repos) == 0 && len(problems) == 0 {
		problems = append(problems, "REPOS is not set (comma-separated list of GitLab/GitHub repository URLs).")
	}
	now := time.Now()
	results := make([]fetchResult, len(repos))
	var wg sync.WaitGroup
	for i, repo := range repos {
		wg.Go(func() {
			results[i].requests, results[i].err = fetch(ctx, repo)
		})
	}
	wg.Wait()
	var groups, terminalGroups []string
	total := 0
	succeeded := false
	_, noColor := os.LookupEnv("NO_COLOR")
	for i, repo := range repos {
		requests, err := results[i].requests, results[i].err
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		succeeded = true
		total += len(requests)
		if len(requests) == 0 {
			continue
		}
		sort.Slice(requests, func(i, j int) bool { return requests[i].CreatedAt.After(requests[j].CreatedAt) })
		lines := []string{fmt.Sprintf("### %s (%d)", markdownText(repo.Path), len(requests))}
		heading := plainText(repo.Path)
		if !noColor {
			heading = "\x1b[1;36m" + heading + "\x1b[0m"
		}
		terminalLines := []string{fmt.Sprintf("▌ %s (%d)", heading, len(requests))}
		for _, request := range requests {
			approved := ""
			if request.Approved {
				approved = " · **approved**"
			}
			lines = append(lines, fmt.Sprintf("- **%s** — *%s*%s · [%s]", requestLink(request), age(request.CreatedAt, now), approved, markdownText(request.Author)))
			title := plainText(request.Title)
			if address := safeUrl(request.Url); address != "" {
				title = "\x1b]8;;" + address + "\x1b\\" + title + "\x1b]8;;\x1b\\"
			} else {
				title += " (" + plainText(request.Url) + ")"
			}
			elapsed, author := age(request.CreatedAt, now), "["+plainText(request.Author)+"]"
			if !noColor {
				title = "\x1b[1;34;4m" + title + "\x1b[0m"
				elapsed = "\x1b[33m" + elapsed + "\x1b[0m"
				author = "\x1b[35m" + author + "\x1b[0m"
			}
			badge := ""
			if request.Approved {
				badge = "  ✓ approved"
				if !noColor {
					badge = "  \x1b[1;32m✓ approved\x1b[0m"
				}
			}
			terminalLines = append(terminalLines, fmt.Sprintf("  • %s  %s%s  %s", title, elapsed, badge, author))
		}
		groups = append(groups, strings.Join(lines, "\n"))
		terminalGroups = append(terminalGroups, strings.Join(terminalLines, "\n"))
	}
	if succeeded {
		output := fmt.Sprintf("Total: %d\n", total)
		if len(groups) > 0 {
			output += "\n" + strings.Join(groups, "\n\n") + "\n"
		}
		if file, ok := out.(*os.File); ok && term.IsTerminal(int(file.Fd())) && os.Getenv("TERM") != "dumb" {
			output = fmt.Sprintf("Total: %d\n", total)
			if len(terminalGroups) > 0 {
				output += "\n" + strings.Join(terminalGroups, "\n\n") + "\n"
			}
		}
		if _, err := io.WriteString(out, output); err != nil {
			problems = append(problems, fmt.Sprintf("write output: %v", err))
		}
	}
	for _, problem := range problems {
		if _, err := fmt.Fprintln(errs, problem); err != nil {
			return 1
		}
	}
	if len(problems) > 0 {
		return 1
	}
	return 0
}
