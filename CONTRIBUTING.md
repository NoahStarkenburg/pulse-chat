# Contributing to Pulse Chat

This file is here because real-world repos have one, and reading your own
contribution rules is a forcing function for code quality. Even as a solo
project, the discipline matters.

## Local development setup

1. Install Go 1.23+: <https://go.dev/dl/>
2. Install Docker Desktop.
3. (Optional) Install `just`: `winget install Casey.Just`
4. (Optional) Install `pre-commit`: `pip install pre-commit && pre-commit install`
5. Clone the repo and run `go mod download`.

Alternative: open the repo in VS Code with the Dev Containers extension
and let it use `.devcontainer/devcontainer.json`.

## Branching

- `main` is always deployable.
- Feature branches: `phase-N/short-description` (see ADR-0002 for rationale).
- One concern per branch.

## Commits

We use [Conventional Commits](https://www.conventionalcommits.org/) for
clarity. The format is:

```
<type>(<scope>): <subject>

<body>
```

Types:
- `feat` — new feature
- `fix` — bug fix
- `refactor` — code change that neither fixes a bug nor adds a feature
- `test` — adding or fixing tests
- `docs` — documentation only
- `chore` — tooling, build scripts, dependencies
- `perf` — performance improvement

Examples:
- `feat(chat): wire WebSocket upgrade handler`
- `fix(hub): unregister was non-idempotent and panicked on double-call`
- `test(config): cover invalid-duration fallback`

Body: explain *why*, not *what*. The diff already shows what.

## Pull requests

Every PR — including yours, even when no one else will review it — goes
through the template at `.github/pull_request_template.md`. The
"What I learned" section is the most important — it's the artifact
that survives the branch.

### Self-review checklist

Before merging your own PR, read your diff cold. Specifically check:

**Correctness**
- [ ] Tests cover the new behavior.
- [ ] Tests cover at least one failure case, not just the happy path.
- [ ] All goroutines have a known stop condition (ctx, channel close, or parent control).
- [ ] No `panic` outside `main`. Errors are returned and wrapped with `%w`.
- [ ] `context.Context` propagates through every I/O call.

**Hygiene**
- [ ] No `fmt.Println` (use `slog`).
- [ ] No `os.Getenv` outside `internal/config`.
- [ ] No global state.
- [ ] Imports are grouped: stdlib / third-party / local.

**Reviewability**
- [ ] Commit messages explain *why*, not *what*.
- [ ] The PR description answers: what, why, what I learned, how I tested.
- [ ] The diff is one logical change. (If you're tempted to write "and also" in the description, split the PR.)

**Production-think**
- [ ] What happens on a slow downstream? (timeout / retry / circuit break)
- [ ] What happens on a duplicate message? (idempotent or guarded)
- [ ] What happens on shutdown mid-request? (graceful drain)
- [ ] What gets logged? (enough to debug, not so much it leaks PII)

If a box can't be checked, write *why* in the PR description.

## Running things locally

```bash
# Build + run the server
just run

# Run tests with race detector
just test

# Run linters
just lint

# Start local infra (Postgres / Redis / RabbitMQ)
just up
```

See `justfile` for the full list of recipes.

## Architecture decisions

Significant decisions live in `decisions/` as ADRs. Before making a
structural change, check whether an ADR already covers it. If you're
overruling an ADR, write a new ADR superseding it.

## Reporting bugs / proposing features

For a solo project, this is mostly a placeholder. If this becomes
collaborative, open a GitHub issue with:

- Steps to reproduce (for bugs)
- Expected vs actual behavior (for bugs)
- Use case (for features)

## License

MIT. By contributing, you agree your contributions are licensed under
the MIT License.
