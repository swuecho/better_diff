# Code Style Guide

Use this guide for all changes in this repository.

## Core Principles
- Optimize for readability and maintainability over cleverness.
- Make behavior explicit; avoid hidden side effects and surprising control flow.
- Keep changes focused and incremental.
- Leave touched code cleaner than before.

## Go Conventions
- Always run `gofmt` (or `go fmt ./...`) before submitting.
- Keep imports grouped and sorted by `gofmt`; do not hand-align.
- Prefer standard Go naming: short receiver names, mixedCaps, exported names only when needed.
- Keep package boundaries clear; avoid cross-package coupling unless it is justified.

## Naming
- Use intention-revealing names (`calculateTotalPrice`, not `calc`).
- Avoid ambiguous placeholders like `data`, `info`, `value`, `tmp`.
- Use domain terms consistently across files and layers.
- Allow short names only for narrow scopes (`i`, `j`, `err`, receiver names).

## Functions and Methods
- Keep functions focused on one responsibility.
- Prefer small functions; if a function exceeds ~30 lines, consider splitting it.
- Keep parameter lists small; avoid boolean flags that change behavior.
- Make mutations obvious from function names and signatures.
- Return early on guard conditions to reduce nesting.

## Error Handling
- Return `error` values; do not use panic for expected failures.
- Wrap errors with context using `%w` when propagating (`fmt.Errorf("load config: %w", err)`).
- Keep error messages lowercase and without trailing punctuation.
- Do not silently ignore errors unless explicitly documented and safe.

## Data and State
- Prefer explicit state transitions over implicit mutation.
- Keep structs cohesive; avoid "god structs" with unrelated fields.
- Pass dependencies explicitly; avoid hidden globals.
- Avoid deep object navigation chains; move behavior closer to the data.

## Comments and Docs
- Write code that is readable without comments first.
- Use comments for intent, invariants, non-obvious constraints, and tradeoffs.
- Keep comments accurate; update or remove stale comments in the same change.
- Remove commented-out code instead of keeping it in source.

## Testing
- Add or update tests for behavior changes and bug fixes.
- Keep tests deterministic and isolated (no time/network/flaky dependencies unless controlled).
- Prefer table-driven tests when validating multiple scenarios.
- Assert externally visible behavior, not implementation details.
- Keep tests readable; clear setup, action, assertion.

## Review Checklist
- Is the change formatted and idiomatic Go?
- Are names and APIs clear and intention-revealing?
- Are responsibilities well-separated with minimal side effects?
- Is error handling contextual, wrapped, and consistent?
- Are tests covering success, failure, and edge cases?
- Did this change reduce or at least not increase technical debt?
