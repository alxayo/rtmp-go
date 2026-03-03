---
agent: "agent"
description: "Post-feature review: optimize, simplify, and validate code quality after implementing a feature"
---

You are reviewing a Go codebase after a feature has been implemented. Your goal is to ensure the code is in its best possible state before merge. Follow the steps below in order, reporting findings and fixing issues as you go.

Read `docs/definition-of-done.md` first to understand the full checklist.

## Step 1: Compilation & Static Analysis

Run these commands and fix any issues:
```
go build ./...
go vet ./...
```

## Step 2: Formatting

Check and fix all Go files:
```
gofmt -l .     # list unformatted files
gofmt -w .     # fix them
```

## Step 3: Dead Code Removal

Search the codebase for:
- Exported types, functions, or constants that are never referenced outside their own file/tests
- Sentinel errors that are declared but never returned by any code path
- Interfaces with methods that no implementation calls

Remove any dead code found. Report what was removed.

## Step 4: Code Simplification

Review all new/modified files for:
- **Duplicated logic**: blocks of 10+ lines repeated in multiple places → extract a helper
- **Redundant checks**: conditions that can never be true given prior logic → remove
- **Over-engineering**: abstractions for one-time operations, unused parameters, speculative future code → simplify
- **Consistency**: new code should follow the same patterns as existing code (error wrapping style, logging conventions, struct layout)

Apply fixes. Report what was simplified with before/after line counts.

## Step 5: Comments & Documentation Accuracy

Review all comments and doc strings in new/modified files:
- Package doc comments: verify counts, lists, and cross-package references are accurate
- Function comments: verify they match current behavior (not stale from before refactoring)
- File references: when referencing code in another package, use full package path
- No escaped quotes in Markdown code blocks

Fix any issues found.

## Step 6: Test Quality

Review all new/modified test files:
- Table-driven tests must use `t.Run(name, ...)` for clear subtest output
- Both `ValidatePublish` and `ValidatePlay` (or equivalent method pairs) are tested
- Coverage gap check: are there behaviors in the production code that no test exercises?
- Handler tests verify end-to-end behavior (e.g., query params stripped from registry keys)

Add missing tests. Report what was added.

## Step 7: Documentation Updates

Verify these docs are updated to reflect the new feature:
- `README.md` (features table, architecture tree, CLI flags, roadmap)
- `docs/getting-started.md` (CLI flags table, usage examples with correct quoting)
- `docs/architecture.md` (package map table)
- `docs/design.md` (design decisions section)
- `docs/implementation.md` (package tree, data flow)
- `.github/copilot-instructions.md` (architecture overview)

Fix any gaps or inconsistencies.

## Step 8: Final Verification

Run the full test suite:
```
go build ./...
go vet ./...
go test ./internal/... -count=1
```

Report the final status: number of packages tested, pass/fail count.

## Step 9: Commit & Push

If any changes were made, commit with appropriate conventional commit messages:
- `refactor(scope):` for code simplification
- `docs:` for documentation fixes  
- `test:` for test improvements
- `style:` for formatting

Push to the feature branch.

## Principles

When making changes, follow these principles from the project's design philosophy:

- **Simplicity over abstraction**: each package does one thing, no framework magic
- **Standard library only**: zero external dependencies
- **Correctness over features**: every byte on the wire must match the RTMP spec
- **No speculative code**: don't add features, refactor code, or make "improvements" beyond what was asked
- **Beginner-friendly comments**: a Go beginner should understand any file in isolation
