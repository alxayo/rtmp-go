# build.ps1 - Build go-rtmp binaries locally (Windows)
# Usage: .\build.ps1 [OPTIONS]
#   -Target server|client|all   Which binary to build (default: all)
#   -OutputDir DIR              Output directory (default: .\bin)
#   -Clean                      Remove existing binaries before building
#   -Race                       Build with race detector
param(
    [ValidateSet("server", "client", "all")]
    [string]$Target = "all",

    [string]$OutputDir,

    [switch]$Clean,
    [switch]$Race
)

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$ProjectRoot = Split-Path -Parent $ScriptDir

if ([string]::IsNullOrWhiteSpace($OutputDir)) {
    $OutputDir = Join-Path $ProjectRoot "bin"
}

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "ERROR: Go is not installed or not on PATH." -ForegroundColor Red
    exit 1
}

New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null

function Build-One {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][string]$PackagePath
    )

    $outPath = Join-Path $OutputDir "$Name.exe"

    if ($Clean -and (Test-Path $outPath)) {
        Remove-Item $outPath -Force
    }

    $args = @("build", "-trimpath")
    if ($Race) {
        $args += "-race"
    }
    $args += @("-o", $outPath, $PackagePath)

    Write-Host "Building $Name..." -ForegroundColor Cyan
    Push-Location $ProjectRoot
    & go @args
    $exitCode = $LASTEXITCODE
    Pop-Location

    if ($exitCode -ne 0) {
        throw "Build failed for $Name"
    }

    Write-Host "Built: $outPath" -ForegroundColor Green
}

Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  go-rtmp - Local Build" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  Target: $Target"
Write-Host "  Output: $OutputDir"
Write-Host ""

if ($Target -eq "server" -or $Target -eq "all") {
    Build-One -Name "rtmp-server" -PackagePath "./cmd/rtmp-server"
}

if ($Target -eq "client" -or $Target -eq "all") {
    Build-One -Name "rtmp-client" -PackagePath "./cmd/rtmp-client"
}

Write-Host ""
Write-Host "Build completed successfully." -ForegroundColor Green
