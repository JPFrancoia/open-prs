# Terminal Markdown plan

Status: Completed (2026-09-29).

## Brief
Render the open-request list as readable Markdown, with color and styled headings in an interactive terminal. Keep output useful when redirected, so users can still pipe or save the listing.

## Current state / relevant context
- The Go CLI prints literal `###` headings, plain titles, and URLs on separate lines. It does not render Markdown.
- Pi's `/open` produces Markdown links, grouped headings, age, approval, and author. `beans`, a nearby Go project, already uses `github.com/charmbracelet/glamour` for terminal Markdown.
- The initial CLI was committed as `dae38e5`. The target repository has no `docs/` folder; the user chose a short root README. The rendering change remains uncommitted.

## Proposed implementation
- Change the existing output template into Markdown links under repository headings. Escape user-supplied titles and paths so Markdown punctuation does not break links or styling.
- Render the fixed list format with a small ANSI formatter when stdout is an interactive terminal. Use OSC 8 escape sequences so each title is clickable without displaying its URL. Emit Markdown links for redirected output or `TERM=dumb`. Keep `NO_COLOR` free of ANSI color styling.
- Preserve the current `REPOS` input, GitHub/GitLab queries, ordering, age and approval fields, error handling, and exit codes. Do not add flags or configuration.

## File-by-file impact
- `go.mod`, `go.sum`: use only terminal detection (`golang.org/x/term`) and its required module. The initial Glamour dependency is removed.
- `internal/openprs/openprs.go`: build Markdown, render to terminal when appropriate, and preserve output write errors.
- `internal/openprs/openprs_test.go`: verify Markdown, clickable terminal titles, `NO_COLOR`, redirected output, and write errors.
- `README.md`: document styled terminal output and Markdown in pipes.
- `plans/terminal-markdown-plan.md`: record implementation and review.

## Risks and edge cases
- Untrusted titles may contain Markdown punctuation or terminal control characters. Escape these before rendering.
- Color in a pipe breaks Markdown consumers. Only style interactive stdout, and honor `NO_COLOR`.
- OSC 8 hyperlinks need terminal support. When a terminal does not support them, pipe the command to see Markdown URLs.
- Glamour prints link URLs beside titles and cannot create title-only hyperlinks. A small formatter for this fixed list avoids its large dependency tree.
- The docs-maintainer role can edit only `docs/`. The user chose a root README, so the parent updated `README.md` directly.
- The first Go review found that a title `x` looked like a Markdown task marker, and the terminal branch lacked direct coverage. Bold links avoid the task marker, and the pseudo-terminal test covers each output mode.
- Rendering must not conceal fetch or output errors. Report errors on stderr and exit nonzero as before.

## Validation / testing
- Run `go test ./...`, `go vet ./...`, `make build`, and an end-to-end run with stubbed `gh` and `glab` executables. Inspect plain piped output and a pseudo-terminal run.
- Run `git diff --check` and the repository hygiene command when available. Run the Go reviewer and address findings.

## Step-by-step execution checklist
- [x] Inspect the current CLI and nearby Go Markdown renderer usage.
- [x] Implement Markdown generation and terminal rendering.
- [x] Test both piped and interactive output, including punctuation, OSC 8 titles, `NO_COLOR`, `TERM=dumb`, and errors.
- [x] Run the Go reviewer and resolve findings. The follow-up review approved the final renderer with no findings.
- [x] Mark this plan completed with the date.

## Open questions / assumptions
- Interactive terminals get styled Markdown; redirects get raw Markdown. This keeps links intact for other tools.
- Use a few fixed ANSI colors for interactive output. No theme options or terminal width settings are needed yet.
