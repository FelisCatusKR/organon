# Issue tracker: GitHub

The backlog and bug reports live in GitHub Issues. Use the `gh` CLI, which infers the repository from the clone.
Implementation plans live in `openspec/changes/`, so break work into tasks there rather than into child issues.
Security problems go through private reporting (see [SECURITY.md](../../SECURITY.md)).

- **Create**: `gh issue create --title "..." --body "..."`. Use a heredoc for multi-line bodies.
- **Read**: `gh issue view <number> --json number,title,body,labels,comments`
- **List**: `gh issue list --state open --json number,title,labels`
- **Comment**: `gh issue comment <number> --body "..."`
- **Close**: `gh issue close <number> --comment "..."`

**PRs as a request surface: no.**

When a skill says "publish to the issue tracker", create a GitHub issue. When it says "fetch the relevant
ticket", read it as above.
