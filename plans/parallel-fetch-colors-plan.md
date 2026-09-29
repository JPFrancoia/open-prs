# Parallel fetch and distinct colors plan

Status: Completed (2026-09-29).

## Brief
Fetch every configured repository at once so slow forge requests do not add their delays together. Make terminal titles, ages, and authors easy to tell apart, and show authors in square brackets.

## Current state / relevant context
- `internal/openprs/openprs.go` fetches repositories one by one. A local test with two 250 ms stub calls took about 512 ms; one call took about 255 ms.
- Pi's `/open` already calls repository CLIs concurrently and preserves the configured order. The terminal output currently uses blue titles and the same dim style for ages and authors.
- The terminal rendering work was committed as `7a9abaa`. The target repository is clean. The user chose a root README, not a `docs/` folder.

## Proposed implementation
- Launch one goroutine per repository and collect each result in a preallocated index-matched slice. Wait for all results, then render in `REPOS` order. Keep each command's 60-second timeout and report each error with its repository.
- Keep clickable titles blue. Show ages in yellow and authors in magenta with literal square brackets. Keep green approvals and cyan repository headings. Use square brackets around authors in redirected Markdown too.
- Keep the existing age calculation from `createdAt`. The request asks for visual distinction, not a change to the date field.

## File-by-file impact
- `internal/openprs/openprs.go`: concurrent fetch collection and small terminal/Markdown format changes.
- `internal/openprs/openprs_test.go`: prove concurrent starts, stable display order, error handling, color separation, and bracketed authors.
- `README.md`: state that repositories fetch concurrently and update the author display description.
- `plans/parallel-fetch-colors-plan.md`: record decisions and completion.

## Risks and edge cases
- Concurrent requests can finish out of order. Store by index and print in configured order.
- A slow or failed repository must not hide successful results. Preserve partial results and nonzero exit on errors.
- An extremely long `REPOS` list launches one process per repository. This is intentional for the requested full parallelism. Add a limit only if a real list causes resource pressure.
- `NO_COLOR` must remove color without removing title hyperlinks or author brackets. Redirected output stays raw Markdown.

## Validation / testing
- Run `go test -race ./...`, `go vet ./...`, `make build`, and stubbed CLI runs with two blocked requests. Check terminal output via a pseudo-terminal.
- Run `git diff --check` and `pre-commit run --all-files` if configured. This repository has no `.pre-commit-config.yaml`. Run the Go reviewer and address actionable findings.

## Step-by-step execution checklist
- [x] Confirm current sequential behavior and inspect the existing output code.
- [x] Implement concurrent requests and distinct output styling.
- [x] Test concurrent starts, stable order, partial failures, colors, and brackets.
- [x] Run Go reviewer and address issues. The reviewer approved with no findings.
- [x] Mark this plan completed with the date.

## Open questions / assumptions
- “Last activity” refers to the currently displayed age. This age is from request creation; it is not a last-update timestamp.
- This change does not install the binary or modify shell configuration. Run `make build` to refresh the binary after editing.
