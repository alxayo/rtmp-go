# check-deps.ps1 — Check availability of required tools for go-rtmp scripts
# Usage: .\check-deps.ps1

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$ProjectRoot = Split-Path -Parent $ScriptDir

$missing = 0

function Check-Tool {
    param(
        [string]$Name,
        [string]$Command,
        [string]$VersionFlag = "--version"
    )

    $cmd = Get-Command $Command -ErrorAction SilentlyContinue
    if ($cmd) {
        try {
            $ver = & $Command $VersionFlag 2>&1 | Select-Object -First 1
        } catch {
            $ver = "(version unknown)"
        }
        Write-Host "[OK] $Name`: $ver" -ForegroundColor Green
    } else {
        Write-Host "[MISSING] $Name`: not found in PATH" -ForegroundColor Red
        $script:missing++
    }
}

Write-Host "=== go-rtmp Dependency Check ===" -ForegroundColor Cyan
Write-Host ""

# Check ffmpeg tools
Check-Tool -Name "ffmpeg"  -Command "ffmpeg"  -VersionFlag "-version"
Check-Tool -Name "ffplay"  -Command "ffplay"  -VersionFlag "-version"
Check-Tool -Name "ffprobe" -Command "ffprobe" -VersionFlag "-version"

# Check Go compiler
Check-Tool -Name "go" -Command "go" -VersionFlag "version"

# Check rtmp-server binary
$binary = Join-Path $ProjectRoot "rtmp-server.exe"
if (-not (Test-Path $binary)) {
    $binary = Join-Path $ProjectRoot "rtmp-server"
}

if (Test-Path $binary) {
    try {
        $ver = & $binary -version 2>&1 | Select-Object -First 1
    } catch {
        $ver = "(version unknown)"
    }
    Write-Host "[OK] rtmp-server: $ver (at $binary)" -ForegroundColor Green
} else {
    Write-Host "[!] rtmp-server: binary not found at $ProjectRoot" -ForegroundColor Yellow
    Write-Host "    Build with: go build -o rtmp-server.exe ./cmd/rtmp-server"
}

Write-Host ""
if ($missing -gt 0) {
    Write-Host "$missing required tool(s) missing. Install them before running tests." -ForegroundColor Red
    exit 1
} else {
    Write-Host "All required tools are available." -ForegroundColor Green
    exit 0
}
