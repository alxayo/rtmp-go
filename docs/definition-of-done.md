# Definition of Done — Feature Completion Checklist

Every feature must pass all items below before it is considered complete and ready for merge.

## 1. Implementation

- [ ] Feature code is implemented per the spec
- [ ] No external dependencies added (stdlib only)
- [ ] New packages have a package-level doc comment explaining purpose and integration points
- [ ] Exported types, functions, and interfaces have godoc comments
- [ ] Code follows existing patterns (error wrapping, logging conventions, concurrency model)

## 2. Code Quality

- [ ] `go build ./...` — compiles with zero errors
- [ ] `go vet ./...` — zero warnings
- [ ] `gofmt` — all files formatted (run: `gofmt -l` to check, `gofmt -w` to fix)
- [ ] No dead code: unused types, functions, constants, or sentinel errors removed
- [ ] No duplicated logic: repeated blocks of 10+ lines extracted into helpers
- [ ] Simplicity over abstraction: no helpers/utilities for one-time operations
- [ ] No speculative code: nothing added "for future use" that isn't called today

## 3. Tests

- [ ] Unit tests for all new packages (>90% coverage target)
- [ ] Table-driven tests use `t.Run(name, ...)` for clear subtest naming
- [ ] Both success and failure paths covered
- [ ] Edge cases tested (nil inputs, empty strings, zero values)
- [ ] Existing tests updated if behavior changed (RPC parsers, handler tests, error tests)
- [ ] All tests pass: `go test ./internal/... -count=1`

## 4. Documentation

### Code Comments
- [ ] Package doc comments accurate (counts, references to other packages correct)
- [ ] No stale comments referencing removed code or old behavior
- [ ] File references use full package paths when crossing package boundaries

### Project Docs (update all that apply)
- [ ] `README.md` — features table, architecture tree, CLI flags section, roadmap
- [ ] `docs/getting-started.md` — CLI flags table, usage examples
- [ ] `docs/architecture.md` — package map table
- [ ] `docs/design.md` — design decisions section for the new feature
- [ ] `docs/implementation.md` — package tree, connection lifecycle, data flow
- [ ] `.github/copilot-instructions.md` — architecture overview tree

### Doc Quality
- [ ] No escaped quotes in Markdown code blocks (`\"` → `"`)
- [ ] Consistent terminology across all docs
- [ ] Examples are copy-pasteable (work as-is in a terminal)

## 5. Git Hygiene

- [ ] Feature branch created from `main`
- [ ] Commits are atomic with descriptive messages following conventional commits:
  - `feat(scope):` for new features
  - `refactor(scope):` for code restructuring
  - `test:` for test improvements
  - `docs:` for documentation-only changes
  - `style:` for formatting-only changes
- [ ] Branch pushed to remote
- [ ] Spec file committed to `specs/` directory

## Verification Commands

Run these in order as a final check:

```bash
# 1. Compile
go build ./...

# 2. Static analysis
go vet ./...

# 3. Formatting
gofmt -l .    # should print nothing

# 4. All tests
go test ./internal/... -count=1

# 5. Full test suite (includes integration — may have pre-existing failures)
go test ./... -count=1
```
