# run-all-tests.ps1 — Run the complete go-rtmp E2E test suite
# Usage: .\run-all-tests.ps1

$ErrorActionPreference = "Continue"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition

Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  go-rtmp - Full E2E Test Suite" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""

# Clean up previous test artifacts
Remove-Item (Join-Path $ScriptDir ".test-tmp") -Recurse -Force -ErrorAction SilentlyContinue
Get-ChildItem (Join-Path $ScriptDir "logs") -Filter "test-*.log" -ErrorAction SilentlyContinue |
    Remove-Item -Force -ErrorAction SilentlyContinue

# Run the E2E test suite
& "$ScriptDir\test-e2e.ps1"
$exitCode = $LASTEXITCODE

Write-Host ""
if ($exitCode -eq 0) {
    Write-Host "All E2E tests completed successfully." -ForegroundColor Green
} else {
    Write-Host "$exitCode test(s) failed." -ForegroundColor Red
}

exit $exitCode
