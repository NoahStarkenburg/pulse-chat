# 0002. Use trunk-based development with phase branches

- **Status:** Accepted
- **Date:** 2026-05-18
- **Deciders:** Noah

## Context

Pulse Chat is solo work today, but the workflow should reflect modern
team practice. The choice of branching model affects: how reviews happen,
how CI runs, how the project narrative is preserved in git history, and
how interview-ready the contribution shape is.

Candidate workflows considered: GitFlow, GitHub Flow / trunk-based,
release branches, no-branch (commit straight to main).

## Decision

Trunk-based development with short-lived feature branches per phase
(or per sub-task within a phase). All work merges to `main` via pull
request. No long-lived `develop` branch.

PR titles follow: `[phase-N] short description`.

## Alternatives considered

### GitFlow
- **Pros:** Clear separation of `develop` / `release` / `main`.
- **Cons:** Heavy for solo work. Long-lived `develop` branch invites
  merge debt. Modern teams have largely moved away from it.
- **Why not:** Overkill; ceremony without payoff.

### Commit straight to main
- **Pros:** Maximum speed.
- **Cons:** No PR = no place to write the "what I learned" reflection
  that the PR template prompts. CI doesn't gate. No reviewable diff.
- **Why not:** Loses the most valuable artifact (PR descriptions as
  learning record).

### Release branches
- **Pros:** Useful when you need to maintain multiple versions in
  parallel.
- **Cons:** Pulse Chat ships continuously to one environment; no need.
- **Why not:** No use case yet.

## Consequences

**Easier:**
- Each phase produces a PR that doubles as a portfolio artifact
  ("here's what I built and what I learned").
- CI gates merges cleanly.
- Encourages small, reviewable changes within each phase.

**Harder:**
- Solo PRs require self-discipline to review your own diff cold
  before merging.
- Slightly more ceremony than committing straight to main.

**Risk accepted:**
- A solo developer can rubber-stamp their own PRs; the discipline only
  works if the self-review is real.
