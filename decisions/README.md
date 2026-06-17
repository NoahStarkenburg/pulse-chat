# Architecture Decision Records

This directory contains ADRs - short documents capturing significant
architectural decisions, the alternatives considered, and the reasoning.

## Why ADRs?

Code shows *what* a system does. Tests show *whether* it works. ADRs show
*why* it's built this way. Six months from now (or in a job interview),
"why did you pick X over Y" is the question that proves you understood
the choice. ADRs are how you remember.

They're also a portfolio artifact. Recruiters who skim source code skip
fast. Recruiters who read an `decisions/` directory linger - it's a
signal of senior thinking.

## Format

We use the lightweight [MADR](https://adr.github.io/madr/) format -
each ADR is a single markdown file with sections for context, decision,
alternatives, and consequences.

Filename convention: `NNNN-kebab-case-title.md`, where NNNN is a zero-padded
sequence number. Never reuse numbers; never reorder. ADRs are append-only.

## Status lifecycle

- **Proposed** - written, not yet approved.
- **Accepted** - current decision; this is what the code reflects.
- **Superseded by ADR-XXXX** - overruled by a later ADR. Don't delete; link.
- **Deprecated** - no longer relevant, but kept for history.

## How to write one

Copy `template.md`, increment the number, fill in the sections, commit.

```
just adr "use sqlc for typed queries"
```

(The justfile recipe handles numbering and file creation.)

## Index

| #   | Title                                       | Status     |
|-----|---------------------------------------------|------------|
| 001 | [Choose Go as the project language](0001-language-choice.md) | Accepted |
| 002 | [Phase-by-phase trunk-based development](0002-trunk-based-dev.md) | Accepted |
