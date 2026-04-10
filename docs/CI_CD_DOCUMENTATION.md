# CI/CD Documentation

This document describes the GitHub Actions workflows and testing strategy for the rtmp-go project.

## Workflows Overview

### 1. CI Workflow (`ci.yml`)
**Purpose**: Fast feedback for every commit and PR
**Triggers**: Push to main/develop, PRs to main
**Duration**: ~5-10 minutes

**Stages**:
- **Quick Checks**: Code formatting, go vet, module verification
- **Core Tests**: Unit tests with race detector, binary build verification
- **Integration Check**: Integration tests and basic server functionality
- **Build Matrix**: Multi-platform build verification (Linux, macOS, Windows)

### 2. Test Workflow (`test.yml`)
**Purpose**: Comprehensive testing including interop tests
**Triggers**: Push to main/develop, PRs to main, manual dispatch
**Duration**: ~20-30 minutes

**Stages**:
- **Golden Vectors**: Generate and validate binary test vectors
- **Unit Tests**: Cross-platform unit tests with coverage (Linux, macOS, Windows)
- **Integration Tests**: Full integration test suite
- **Interop Tests**: FFmpeg compatibility tests
- **Build Validation**: Cross-platform build verification
- **Benchmarks**: Performance benchmarks (main branch only)

### 3. Quality Workflow (`quality.yml`)
**Purpose**: Code quality, security, and static analysis
**Triggers**: Push to main/develop, PRs to main, manual dispatch
**Duration**: ~10-15 minutes

**Stages**:
- **Code Quality**: staticcheck, govulncheck, formatting, go vet
- **Test Structure**: Validate test organization and coverage
- **Dependency Analysis**: Check for external dependencies (project aims for stdlib-only)
- **Documentation**: Validate documentation completeness
- **Binary Analysis**: Security-focused binary builds and analysis
- **Cross-compilation**: Test builds for multiple platforms/architectures

### 4. Build Workflow (`build.yml`)
**Purpose**: Multi-platform binary builds and releases
**Triggers**: Push to main/develop, tags, PRs to main
**Duration**: ~15-20 minutes

**Features**:
- Multi-platform builds (Windows, macOS, Linux) for multiple architectures
- Automated releases for tags
- Artifact uploads with build information
- Build summaries with artifact sizes

### 5. Interop Workflow (`interop.yml`) 
**Purpose**: FFmpeg interoperability testing
**Triggers**: Changes to internal/cmd code, manual dispatch
**Duration**: ~15-20 minutes

**Features**:
- FFmpeg installation and testing
- Multiple interop scenarios (publish, play, concurrency, recording)
- Recording artifact uploads

## Test Strategy

### Test Types

| Test Type | Location | Purpose | Duration |
|-----------|----------|---------|----------|
| **Unit Tests** | `internal/.../*_test.go` | Component-level testing | Fast (~2-5 min) |
| **Integration Tests** | `tests/integration/` | Cross-component workflows | Medium (~5-10 min) |
| **Golden Vector Tests** | `tests/golden/` | Protocol compliance validation | Fast (~1-2 min) |
| **Interop Tests** | `tests/interop/` | FFmpeg/OBS compatibility | Medium (~5-15 min) |
| **Benchmarks** | `*_test.go` | Performance validation | Fast (~2-5 min) |

### Test Execution

```bash
# Run all tests locally (same as CI)
make ci

# Run specific test types
make test-unit           # Unit tests only
make test-integration    # Integration tests only  
make test-interop        # FFmpeg interop tests
make benchmark           # Performance benchmarks

# Generate test coverage
make coverage
```

### Test Requirements

**Unit Tests**:
- Must use `-race` detector
- Require >80% coverage for core packages
- Use golden test vectors for protocol validation
- Test both success and error cases

**Integration Tests**:
- Test full RTMP workflows (handshake → connect → publish/play)
- Use `net.Pipe` for deterministic networking
- Validate expected log output and state transitions
- Test concurrent connections and error handling

**Interop Tests**:
- Test with real FFmpeg/ffplay
- Validate publish and playback scenarios
- Test concurrent streams and failure isolation
- Generate and validate recordings

## Quality Gates

### Required for Merge
- ✅ All CI checks pass (quick-checks, core-tests, build-matrix)
- ✅ Code formatting (go fmt)
- ✅ Static analysis (go vet, staticcheck)
- ✅ Unit tests pass with race detector
- ✅ Multi-platform builds succeed

### Optional but Recommended
- ✅ Integration tests pass
- ✅ FFmpeg interop tests pass
- ✅ Security scan passes (govulncheck)
- ✅ Test coverage >80% for core packages

### Release Requirements
- ✅ All quality checks pass
- ✅ All test suites pass
- ✅ Benchmarks show no regressions
- ✅ Documentation is complete
- ✅ Cross-compilation for all target platforms

## Local Development

### Quick Development Cycle
```bash
# Set up development environment
make dev-setup

# Development test cycle
make dev-test

# Run CI checks locally before pushing
make ci
```

### Troubleshooting Tests

```bash
# Run specific failing test
go test -race -v ./internal/rtmp/handshake -run TestSpecificFunction

# Run with more verbose output
go test -race -v -count=1 ./tests/integration/... -timeout=20m

# Run interop tests with debug logging
SERVER_FLAGS="-log-level debug" make test-interop

# Generate test coverage for specific package
go test -coverprofile=pkg.out ./internal/rtmp/chunk
go tool cover -html=pkg.out
```

### Adding New Tests

1. **Unit Tests**: Add `*_test.go` files next to implementation
2. **Integration Tests**: Add to `tests/integration/` with descriptive names
3. **Golden Vectors**: Add generation logic to `tests/golden/gen_*.go`
4. **Interop Tests**: Extend `tests/interop/ffmpeg_test.sh` with new scenarios

## GitHub Actions Configuration

### Secrets Required
Currently no secrets are required. The workflows use only public actions and built-in GitHub tokens.

### Future Enhancements
- Code coverage reporting integration
- Automated dependency updates
- Performance regression detection
- Security scanning integration
- Release automation improvements

## Monitoring and Observability

### Workflow Monitoring
- GitHub Actions dashboard shows workflow status
- Workflow summaries provide detailed results
- Artifacts are retained for debugging
- Build summaries include binary sizes and build information

### Performance Tracking
- Benchmark results are archived for performance tracking
- Binary size monitoring for release optimization
- Test execution time tracking for CI optimization

---

For questions or improvements to the CI/CD setup, please open an issue or discussion.