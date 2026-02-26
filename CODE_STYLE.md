# Clean Code Guidelines

Use this checklist for all changes in this repository.

## Naming
- Use intention-revealing names (`calculateTotalPrice`, not `calc`).
- Avoid disinformation (do not imply an incorrect type or behavior).
- Avoid meaningless distinctions (`data`, `info`, `value` without context).
- Prefer pronounceable and searchable names.
- Avoid single-letter identifiers except short loop counters.

## Functions
- Keep functions small and focused on one responsibility.
- Scrutinize functions longer than ~20 lines.
- Prefer 0-2 arguments where possible.
- Avoid flag arguments.
- Avoid hidden side effects.
- Use exceptions/errors over magic return codes.

## Comments
- Prefer self-explanatory code over explanatory comments.
- Do not add obvious comments.
- Use comments for intent, constraints, warnings, or legal context.
- Remove commented-out code.

## Formatting
- Keep related concepts physically close.
- Do not overuse horizontal alignment.
- Follow team consistency over personal preference.

## Objects and Data Structures
- Prefer "tell, don't ask" behavior.
- Follow Law of Demeter (avoid deep object navigation chains).

## Error Handling
- Use errors for exceptional paths, not normal control flow.
- Add contextual information to errors.
- Avoid returning or passing `nil` where a safer alternative exists.

## Testing
- Follow FIRST: Fast, Independent, Repeatable, Self-validating, Timely.
- Keep each test focused on one behavior/concept.
- Keep tests readable and maintainable.

## General
- Follow the Boy Scout Rule: leave code cleaner than you found it.
- Apply DRY thoughtfully; prefer duplication over bad abstraction.
- Favor open/closed design: open for extension, closed for modification.
- Depend on abstractions, not concretions.

## PR Review Checklist
- Are names intention-revealing and unambiguous?
- Does each function do one thing?
- Are side effects explicit and justified?
- Are comments necessary and intent-focused?
- Is error handling contextual and consistent?
- Do tests clearly verify behavior and remain readable?
- Is the change cleaner than the previous version?
