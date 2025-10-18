# Build Guide for RTMP-Go

This guide explains how to use the GitHub Actions workflow (`build.yml`) to build the RTMP application for multiple platforms and architectures.

## Overview

The `build.yml` workflow automatically builds your RTMP server application for the following platforms:

| Platform | Architecture | Binary Name |
|----------|--------------|-------------|
| Windows | x86_64 (AMD64) | `rtmp-server-windows-x86_64.exe` |
| Windows | ARM64 | `rtmp-server-windows-arm64.exe` |
| macOS | ARM64 (Apple Silicon) | `rtmp-server-macos-arm64` |
| Linux | x86_64 (AMD64) | `rtmp-server-linux-x86_64` |
| Linux | ARM64 | `rtmp-server-linux-arm64` |

## Workflow Features

### ✅ Automated Multi-Platform Builds
- Builds for 5 different platform/architecture combinations
- Uses Go cross-compilation for efficient builds
- Optimized binaries with reduced size (`-ldflags="-w -s"`)

### ✅ Comprehensive Testing
- Runs full test suite on Linux x86_64
- Validates code before building binaries

### ✅ Artifact Management
- Each build is uploaded as a separate artifact
- Includes build metadata (commit, date, Go version)
- 30-day artifact retention

### ✅ Automatic Releases
- Creates GitHub releases for version tags
- Platform-specific packaging (ZIP for Windows, TAR.GZ for Unix)
- Release notes generation

### ✅ Build Summaries
- Visual build status in GitHub Actions UI
- Artifact size information
- Comprehensive build metadata

## How to Use the Build Workflow

### 1. Trigger Builds Automatically

The workflow runs automatically when:

#### Push to Main Branches
```bash
git push origin main
# or
git push origin develop
```

#### Pull Requests
```bash
# Create a PR to main branch - builds will run automatically
git checkout -b feature/my-feature
git push origin feature/my-feature
# Then create PR via GitHub UI
```

#### Version Tags (Creates Releases)
```bash
# Tag a version and push
git tag v1.0.0
git push origin v1.0.0

# This will:
# 1. Build for all platforms
# 2. Create a GitHub release
# 3. Upload all binaries as release assets
```

### 2. Manual Workflow Trigger

You can manually trigger builds from the GitHub UI:

1. Go to your repository on GitHub
2. Click **Actions** tab
3. Select **Build Multi-Platform** workflow
4. Click **Run workflow** button
5. Choose the branch and click **Run workflow**

### 3. Download Build Artifacts

#### From Workflow Runs
1. Go to **Actions** tab in your GitHub repository
2. Click on a completed workflow run
3. Scroll down to **Artifacts** section
4. Download the platform-specific artifact you need

#### From Releases (for tagged versions)
1. Go to **Releases** section in your GitHub repository
2. Find the release version you want
3. Download the appropriate binary for your platform:
   - Windows: `rtmp-server-windows-*.zip`
   - macOS: `rtmp-server-macos-*.tar.gz`
   - Linux: `rtmp-server-linux-*.tar.gz`

### 4. Version Release Workflow

To create a new release with binaries:

```bash
# 1. Ensure your code is ready for release
git checkout main
git pull origin main

# 2. Create and push a version tag
git tag v1.2.0
git push origin v1.2.0

# 3. The workflow will automatically:
#    - Build for all platforms
#    - Run tests
#    - Create GitHub release
#    - Upload binaries as release assets
```

#### Version Tag Formats
- **Stable releases**: `v1.0.0`, `v2.1.3`
- **Pre-releases**: `v1.0.0-alpha`, `v1.0.0-beta.1`, `v1.0.0-rc.1`

Pre-release tags will be marked as "pre-release" in GitHub.

## Build Configuration

### Environment Variables
- **Go Version**: `1.25.1` (configurable in workflow)
- **CGO**: Disabled for static binaries
- **Build Flags**: `-ldflags="-w -s"` for optimized binaries

### Build Optimization
The workflow creates optimized binaries by:
- Disabling CGO (`CGO_ENABLED=0`) for static linking
- Stripping debug information (`-w -s` flags)
- Using Go's efficient cross-compilation

## Troubleshooting

### Build Failures

#### Check Workflow Status
1. Go to **Actions** tab
2. Click on the failed workflow run
3. Expand the failed job to see error details

#### Common Issues
- **Test failures**: Fix failing tests before builds will succeed
- **Dependency issues**: Ensure `go.mod` is up to date
- **Cross-compilation errors**: Check for platform-specific code issues

#### Fix and Retry
```bash
# Fix the issue, then:
git add .
git commit -m "Fix build issue"
git push origin main  # This will trigger a new build
```

### Missing Artifacts

If artifacts are missing:
1. Check that the workflow completed successfully
2. Verify you're looking at the correct workflow run
3. Artifacts expire after 30 days

### Release Issues

If releases aren't created:
1. Ensure you pushed a tag (not just created it locally)
2. Check that the tag follows the `v*` format
3. Verify you have proper permissions for creating releases

## Build Information

Each build artifact includes a `build-info.txt` file with:
- Target OS and architecture
- Go version used
- Git commit hash
- Build timestamp
- Branch name

## Local Development

For local development and testing, you can still build manually:

```bash
# Build for current platform
go build -o rtmp-server ./cmd/rtmp-server

# Cross-compile for specific platform
GOOS=windows GOARCH=amd64 go build -o rtmp-server.exe ./cmd/rtmp-server
GOOS=linux GOARCH=arm64 go build -o rtmp-server-arm64 ./cmd/rtmp-server
GOOS=darwin GOARCH=arm64 go build -o rtmp-server-macos ./cmd/rtmp-server
```

## Integration with CI/CD

The build workflow integrates seamlessly with your development process:

1. **Development**: Push to feature branches
2. **Integration**: Create PRs to `main` (triggers builds and tests)
3. **Release**: Tag versions to create releases with binaries
4. **Distribution**: Download binaries from GitHub releases

This ensures every release has tested, ready-to-use binaries for all supported platforms.