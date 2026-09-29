package entities

import "time"

// Repo identifies a GitHub or GitLab repository.
type Repo struct {
	Host  string
	Path  string
	Forge string
}

// Request is an open pull or merge request.
type Request struct {
	Title     string
	Url       string
	CreatedAt time.Time
	Author    string
	Approved  bool
}
