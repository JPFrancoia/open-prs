package openprs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ent "open-prs/internal/entities"
)

// TestRun checks the real CLI command path with local forge executables.
func TestRun(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"gh": `#!/bin/sh
printf '%s' '[{"title":"Old","url":"https://github.com/a/b/pull/1","createdAt":"2024-01-01T00:00:00Z","author":{"login":"alice"},"reviewDecision":"APPROVED"},{"title":"New","url":"https://github.com/a/b/pull/2","createdAt":"2024-01-02T00:00:00Z","author":null}]'
`,
		"glab": `#!/bin/sh
printf '%s' '{"data":{"project":{"mergeRequests":{"nodes":[{"title":"Fix","webUrl":"https://gitlab.com/g/r/-/merge_requests/1","createdAt":"2024-01-03T00:00:00Z","approved":true,"author":{"username":"bob"}}]}}}}'
`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var out, errs bytes.Buffer
	status := Run(context.Background(), "https://github.com/a/b.git,gitlab.com/g/r,https://example.com/a/b", &out, &errs)
	if status != 1 || !strings.Contains(errs.String(), "Unsupported host") {
		t.Fatalf("status %d, stderr %q", status, errs.String())
	}
	text := out.String()
	for _, want := range []string{"### a/b", "### g/r", "- **[New](<https://github.com/a/b/pull/2>)", "[Old](<https://github.com/a/b/pull/1>)", "[Fix](<https://gitlab.com/g/r/-/merge_requests/1>)", "**approved** · alice", "**approved** · bob", "· unknown"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q from %q", want, text)
		}
	}
	if strings.Index(text, "[New]") > strings.Index(text, "[Old]") || strings.Index(text, "### g/r") < strings.Index(text, "### a/b") {
		t.Errorf("unexpected order: %q", text)
	}
}

// TestMarkdownLink escapes untrusted titles and rejects non-HTTP link schemes.
func TestMarkdownLink(t *testing.T) {
	request := ent.Request{Title: "Fix [api] *now*\x1b[31m", Url: "https://github.com/a/b/pull/1"}
	if got, want := requestLink(request), "[Fix \\[api\\] \\*now\\* \\[31m](<https://github.com/a/b/pull/1>)"; got != want {
		t.Fatalf("link %q, want %q", got, want)
	}
	request.Url = "javascript:alert(1)"
	if got := requestLink(request); strings.Contains(got, "](<") || !strings.Contains(got, "javascript:alert(1)") {
		t.Fatalf("unsafe URL rendered as link: %q", got)
	}
}

// TestTerminalRendering exercises Run through a pseudo-terminal and a pipe.
func TestTerminalRendering(t *testing.T) {
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("script is needed to test a pseudo-terminal")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "open-prs")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/server")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %s: %v", output, err)
	}
	content := "#!/bin/sh\necho '[{\"title\":\"x\",\"url\":\"https://github.com/a/b/pull/1\",\"createdAt\":\"2024-01-01T00:00:00Z\"}]'\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH"), "REPOS=github.com/a/b", "TERM=xterm-256color"}
	for _, test := range []struct {
		name     string
		terminal bool
		env      []string
		styled   bool
	}{
		{name: "terminal", terminal: true, styled: true},
		{name: "pipe"},
		{name: "no color", terminal: true, env: []string{"NO_COLOR=1"}},
		{name: "dumb terminal", terminal: true, env: []string{"TERM=dumb"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var command *exec.Cmd
			if test.terminal {
				command = exec.Command("script", "-q", "-c", binary, "/dev/null")
			} else {
				command = exec.Command(binary)
			}
			command.Env = append(append([]string{}, env...), test.env...)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("run CLI: %s: %v", output, err)
			}
			if strings.Contains(string(output), "\x1b[") != test.styled {
				t.Errorf("styled = %v, want %v: %q", strings.Contains(string(output), "\x1b["), test.styled, output)
			}
			if test.terminal && test.name != "dumb terminal" {
				if !strings.Contains(string(output), "\x1b]8;;https://github.com/a/b/pull/1\x1b\\x\x1b]8;;\x1b\\") || strings.Contains(string(output), "[x](<") {
					t.Errorf("title is not a clickable terminal link: %q", output)
				}
			} else if !strings.Contains(string(output), "- **[x](<https://github.com/a/b/pull/1>)**") {
				t.Errorf("missing Markdown link: %q", output)
			}
		})
	}
}

// TestParseRepos accepts hostnames regardless of DNS letter case.
func TestParseRepos(t *testing.T) {
	repos, problems := parseRepos("https://GitHub.com/a/b")
	if len(problems) != 0 || len(repos) != 1 || repos[0].Host != "github.com" {
		t.Fatalf("repos %v, errors %v", repos, problems)
	}
}

// brokenWriter models an output destination that cannot accept data.
type brokenWriter struct{}

// Write always fails to simulate a closed output destination.
func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("closed output") }

// TestWriteFailure checks that lost terminal output causes failure.
func TestWriteFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\necho '[{\"title\":\"Fix\",\"createdAt\":\"2024-01-01T00:00:00Z\"}]'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var errs bytes.Buffer
	if status := Run(context.Background(), "github.com/a/b", brokenWriter{}, &errs); status != 1 || !strings.Contains(errs.String(), "closed output") {
		t.Fatalf("status %d, stderr %q", status, errs.String())
	}
}

// TestInvalidTimestamp rejects malformed forge output.
func TestInvalidTimestamp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\necho '[{\"title\":\"Fix\",\"createdAt\":\"invalid\"}]'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var out, errs bytes.Buffer
	if status := Run(context.Background(), "github.com/a/b", &out, &errs); status != 1 || !strings.Contains(errs.String(), "parse gh output") || out.Len() != 0 {
		t.Fatalf("status %d, stdout %q, stderr %q", status, out.String(), errs.String())
	}
}

// TestEmptyAndFailedCommands checks missing input and partial command failures.
func TestEmptyAndFailedCommands(t *testing.T) {
	var out, errs bytes.Buffer
	if Run(context.Background(), "", &out, &errs) != 1 || !strings.Contains(errs.String(), "REPOS is not set") {
		t.Fatalf("missing configuration: %q", errs.String())
	}
	t.Setenv("PATH", t.TempDir())
	errs.Reset()
	if Run(context.Background(), "github.com/a/b", &out, &errs) != 1 || !strings.Contains(errs.String(), "run gh") {
		t.Fatalf("missing gh: %q", errs.String())
	}
}

// TestAge checks the same age cutoffs used by /open.
func TestAge(t *testing.T) {
	now := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct{ created, want string }{
		{"2026-01-09T23:30:00Z", "just now"},
		{"2026-01-09T12:00:00Z", "12h ago"},
		{"2026-01-09T00:00:00Z", "1d ago"},
	} {
		created, err := time.Parse(time.RFC3339, test.created)
		if err != nil {
			t.Fatal(err)
		}
		if got := age(created, now); got != test.want {
			t.Errorf("age(%q) = %q, want %q", test.created, got, test.want)
		}
	}
}
