# Open PRs CLI plan

Status: Completed (2026-09-29).

## Brief
Build a small Go command that prints the same open pull and merge requests as Pi's `/open` command. This gives the existing repository list a terminal interface without adding a new API client or configuration system.

## Current state / relevant context
- The target repository is empty. `extensions/pi-open.ts` in `skills_and_commands` defines the existing behavior.
- `/open` reads comma-separated repository URLs from `REPOS`. It calls `gh pr list` for GitHub and `glab api graphql` for GitLab, shows title, URL, age, author, and approval, and sorts each repository newest first.
- `gh` is installed locally; `glab` is not installed. The command must report missing tools without hiding results from other repositories.

## Proposed implementation
- Use Go standard library plus the existing `gh` and `glab` executables. Do not add dependencies.
- Keep the entry point in `cmd/server/main.go` and put parsing, command calls, and terminal output in one private package. Keep JSON response structs local to that package as external protocol types.
- Accept `REPOS` as the only input. Print plain terminal text grouped by repository with the same fields as `/open`. Include clickable URLs as text. Report invalid repositories, failed commands, and malformed responses to stderr; exit nonzero for partial failures.
- Bound each external command to 60 seconds and preserve the configured repository order.

## File-by-file impact
- `go.mod`: define a local module without external dependencies.
- `cmd/server/main.go`: minimal CLI entry point and exit status.
- `internal/entities/request.go`: store repository and request models, including parsed creation times.
- `internal/openprs/openprs.go`: parse repositories, query `gh` or `glab`, decode results, format output and errors.
- `internal/openprs/openprs_test.go`: check parsing, decoding, ordering, write failures, and errors with stub executables.
- `README.md`: document build, setup, and use. The user chose a root README instead of a `docs/` folder.
- `plans/open-prs-cli-plan.md`: track decisions and completion.

## Risks and edge cases
- GitLab CLI may be absent or unauthenticated. Keep errors visible and continue to other repositories.
- The source limits each forge query to 100 requests. Retain this limit rather than adding pagination.
- GraphQL project paths require quoting. Serialize the path as a JSON string before interpolation.
- Empty `REPOS` must produce a helpful error. A valid but empty repository prints nothing.
- Go review found that malformed timestamps and failed output writes could return success. Reject malformed timestamps and return status 1 for write failures. The follow-up Go review approved the fixes with no findings.

## Validation / testing
- Run `go test ./...`, `go vet ./...`, `go build ./cmd/server`, and the built binary with stub `gh` and `glab` commands and a sample `REPOS`.
- Run `git diff --check`, then run the `go-reviewer` agent on the added Go files and address actionable findings. `pre-commit run --all-files` cannot run because this new repository has no `.pre-commit-config.yaml`.

## Step-by-step execution checklist
- [x] Inspect `/open`, Go conventions, and the empty target repository.
- [x] Implement the CLI and focused tests.
- [x] Run Go checks and an end-to-end stub command.
- [x] Run Go reviewer and resolve issues.
- [x] Mark this plan completed with the date.

## Open questions / assumptions
- A terminal-oriented text listing is sufficient. No Markdown renderer, flags, or installation step is requested.
- The user chose a concise root README instead of creating a `docs/` tree.
