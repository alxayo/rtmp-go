# ============================================================================
# run-camera-tests.ps1 — Run only camera E2E tests (Windows)
#
# These tests require a live camera device (Windows: dshow, macOS: avfoundation).
# Tests will auto-skip if no camera is detected on the current platform.
#
# USAGE:
#   .\e2e-tests\run-camera-tests.ps1               # Run all camera tests
#   .\e2e-tests\run-camera-tests.ps1 -List          # List camera tests
#   .\e2e-tests\run-camera-tests.ps1 -Filter srt    # Filter by pattern
# ============================================================================
param(
    [string]$Filter = "",
    [switch]$List
)

$ErrorActionPreference = "Continue"
$ScriptDir = $PSScriptRoot

. "$ScriptDir\_lib.ps1"

# Discover camera test scripts only
$tests = Get-ChildItem "$ScriptDir\camera-*.ps1" -File |
    Where-Object { -not $Filter -or $_.Name -match $Filter } |
    Sort-Object Name

Write-Host "============================================" -ForegroundColor Blue
Write-Host "  go-rtmp - Camera E2E Tests" -ForegroundColor Blue
Write-Host "============================================" -ForegroundColor Blue
Write-Host ""
Write-Host "Tests found: $($tests.Count)"
if ($Filter) { Write-Host "Filter: $Filter" }
Write-Host ""

if ($List) {
    foreach ($t in $tests) { Write-Host "  $($t.Name)" }
    exit 0
}

Build-Server

Remove-Item "$ScriptDir\.test-tmp" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item "$ScriptDir\logs\*.log" -Force -ErrorAction SilentlyContinue

$total = 0; $passed = 0; $failed = 0; $skipped = 0
$failedTests = @()

foreach ($testScript in $tests) {
    $total++
    $testName = $testScript.BaseName

    try {
        & $testScript.FullName
        if ($LASTEXITCODE -eq 0) { $passed++ }
        elseif ($LASTEXITCODE -eq 2) { $skipped++ }
        else { $failed++; $failedTests += $testName }
    } catch {
        $failed++; $failedTests += $testName
    }
}

Write-Host ""
Write-Host "=== Camera Test Suite Summary ===" -ForegroundColor Blue
Write-Host "  Total:   $total"
Write-Host "  Passed:  $passed" -ForegroundColor Green
Write-Host "  Failed:  $failed" -ForegroundColor Red
Write-Host "  Skipped: $skipped" -ForegroundColor Yellow

if ($failed -gt 0) {
    Write-Host ""
    Write-Host "Failed tests:" -ForegroundColor Red
    foreach ($t in $failedTests) { Write-Host "  X $t" -ForegroundColor Red }
    exit 1
} else {
    Write-Host ""
    Write-Host "All camera tests passed!" -ForegroundColor Green
    exit 0
}
