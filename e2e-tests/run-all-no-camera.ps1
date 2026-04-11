$ErrorActionPreference = "Stop"

# ============================================================================
# Run all E2E tests WITHOUT camera tests (for CI/headless environments)
#
# USAGE:
#   .\e2e-tests\run-all-no-camera.ps1 [-Filter PATTERN]
# ============================================================================

param(
    [string]$Filter = ""
)

# Set environment variable to skip camera tests
$env:SKIP_CAMERA_TESTS = "1"

# Build arguments for run-all.ps1
$args = @()
if ($Filter) {
    $args += "--filter"
    $args += $Filter
}

# Run all tests via run-all.ps1
& "$PSScriptRoot\run-all.ps1" @args
