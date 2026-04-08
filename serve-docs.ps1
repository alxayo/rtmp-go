# serve-docs.ps1 — Build and serve the go-rtmp documentation site locally.
# Usage: .\serve-docs.ps1 [-BuildOnly] [-Port 1313]
param(
    [switch]$BuildOnly,
    [int]$Port = 1313
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$SiteDir = Join-Path $ScriptDir "site"

# --- Locate Hugo ---
$hugo = Get-Command hugo -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source

if (-not $hugo) {
    # Check common WinGet install path
    $wingetPath = Join-Path $env:LOCALAPPDATA "Microsoft\WinGet\Packages"
    $candidates = Get-ChildItem $wingetPath -Recurse -Filter "hugo.exe" -ErrorAction SilentlyContinue
    if ($candidates) {
        $hugo = $candidates[0].FullName
    }
}

if (-not $hugo) {
    Write-Host "Hugo not found. Installing via winget..." -ForegroundColor Yellow
    winget install Hugo.Hugo.Extended --accept-package-agreements --accept-source-agreements
    # Refresh PATH
    $env:PATH = [System.Environment]::GetEnvironmentVariable("PATH", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("PATH", "User")
    $hugo = Get-Command hugo -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source
    if (-not $hugo) {
        Write-Error "Hugo installation failed. Install manually: https://gohugo.io/installation/"
        exit 1
    }
}

Write-Host "Using: $(& $hugo version)" -ForegroundColor Cyan

# --- Init submodule (theme) ---
$themeMarker = Join-Path $SiteDir "themes\hugo-book\theme.toml"
if (-not (Test-Path $themeMarker)) {
    Write-Host "Initializing hugo-book theme submodule..." -ForegroundColor Yellow
    git -C $ScriptDir submodule update --init --recursive
}

# --- Build ---
Write-Host "Building site..." -ForegroundColor Cyan
& $hugo --source $SiteDir --gc --minify
Write-Host "Build complete: $SiteDir\public\" -ForegroundColor Green

if ($BuildOnly) {
    Write-Host "Done (build-only mode)."
    exit 0
}

# --- Serve ---
Write-Host ""
Write-Host "Starting local server on http://localhost:$Port/" -ForegroundColor Green
Write-Host "Press Ctrl+C to stop."
& $hugo server --source $SiteDir --port $Port --buildDrafts --navigateToChanged
