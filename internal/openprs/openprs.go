package openprs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"time"

	ent "open-prs/internal/entities"
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

// Run prints the configured open requests and returns a nonzero status on errors.
func Run(ctx context.Context, value string, out, errs io.Writer) int {
	repos, problems := parseRepos(value)
	if len(repos) == 0 && len(problems) == 0 {
		problems = append(problems, "REPOS is not set (comma-separated list of GitLab/GitHub repository URLs).")
	}
	now := time.Now()
repositories:
	for _, repo := range repos {
		requests, err := fetch(ctx, repo)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if len(requests) == 0 {
			continue
		}
		sort.Slice(requests, func(i, j int) bool { return requests[i].CreatedAt.After(requests[j].CreatedAt) })
		if _, err := fmt.Fprintf(out, "### %s\n", repo.Path); err != nil {
			problems = append(problems, fmt.Sprintf("write %s: %v", repo.Path, err))
			break
		}
		for _, request := range requests {
			approved := ""
			if request.Approved {
				approved = " (approved)"
			}
			if _, err := fmt.Fprintf(out, "- %s — %s%s [%s]\n  %s\n", request.Title, age(request.CreatedAt, now), approved, request.Author, request.Url); err != nil {
				problems = append(problems, fmt.Sprintf("write %s: %v", repo.Path, err))
				break repositories
			}
		}
		if _, err := fmt.Fprintln(out); err != nil {
			problems = append(problems, fmt.Sprintf("write %s: %v", repo.Path, err))
			break
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
