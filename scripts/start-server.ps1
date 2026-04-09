# start-server.ps1 — Start the go-rtmp server with configurable options
# Usage: .\start-server.ps1 [OPTIONS]
#   -Mode plain|tls|both     Transport mode (default: plain)
#   -EnableHLS               Enable HLS conversion hook on publish
#   -EnableAuth              Enable token auth with test tokens
#   -Port PORT               RTMP listen port (default: 1935)
#   -TLSPort PORT            RTMPS listen port (default: 1936)
#   -LogLevel LEVEL          Log level: debug|info|warn|error (default: info)
#   -Foreground              Run in foreground (default: background)
param(
    [ValidateSet("plain", "tls", "both")]
    [string]$Mode = "plain",
    [switch]$EnableHLS,
    [switch]$EnableAuth,
    [int]$Port = 1935,
    [int]$TLSPort = 1936,
    [ValidateSet("debug", "info", "warn", "error")]
    [string]$LogLevel = "info",
    [switch]$Foreground
)

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$ProjectRoot = Split-Path -Parent $ScriptDir

# Check dependencies
Write-Host "Checking dependencies..." -ForegroundColor Cyan
& "$ScriptDir\check-deps.ps1"
if ($LASTEXITCODE -ne 0) { exit 1 }
Write-Host ""

# Build server if needed
$Binary = Join-Path $ProjectRoot "rtmp-server.exe"
$needsBuild = -not (Test-Path $Binary)
if (-not $needsBuild) {
    $binaryTime = (Get-Item $Binary).LastWriteTime
    $newerSources = Get-ChildItem -Path "$ProjectRoot\cmd", "$ProjectRoot\internal" -Recurse -Filter "*.go" |
        Where-Object { $_.LastWriteTime -gt $binaryTime } | Select-Object -First 1
    if ($newerSources) { $needsBuild = $true }
}

if ($needsBuild) {
    Write-Host "Building rtmp-server..." -ForegroundColor Cyan
    Push-Location $ProjectRoot
    & go build -o $Binary ./cmd/rtmp-server
    if ($LASTEXITCODE -ne 0) { Pop-Location; throw "Build failed" }
    Pop-Location
    Write-Host "Built: $Binary"
}

# Generate certs if TLS mode
if ($Mode -eq "tls" -or $Mode -eq "both") {
    $CertFile = Join-Path $ScriptDir ".certs\cert.pem"
    $KeyFile = Join-Path $ScriptDir ".certs\key.pem"
    if (-not (Test-Path $CertFile) -or -not (Test-Path $KeyFile)) {
        Write-Host "Generating TLS certificates..." -ForegroundColor Cyan
        & "$ScriptDir\generate-certs.ps1"
    }
}

# Build command arguments
$args_list = @("-listen", ":$Port", "-log-level", $LogLevel)

if ($Mode -eq "tls" -or $Mode -eq "both") {
    $args_list += "-tls-listen", ":$TLSPort"
    $args_list += "-tls-cert", (Join-Path $ScriptDir ".certs\cert.pem")
    $args_list += "-tls-key", (Join-Path $ScriptDir ".certs\key.pem")
}

if ($EnableHLS) {
    $hookScript = Join-Path $ScriptDir "on-publish-hls.ps1"
    $args_list += "-hook-script", "publish_start=$hookScript"
}

if ($EnableAuth) {
    $args_list += "-auth-mode", "token"
    $args_list += "-auth-token", "live/test=secret123"
    $args_list += "-auth-token", "live/secure=mytoken456"
}

# Create log directory
$LogDir = Join-Path $ScriptDir "logs"
New-Item -ItemType Directory -Path $LogDir -Force | Out-Null
$LogFile = Join-Path $LogDir "rtmp-server.log"

Write-Host "=== Starting go-rtmp Server ===" -ForegroundColor Cyan
Write-Host "  Mode:     $Mode"
Write-Host "  RTMP:     :$Port"
if ($Mode -eq "tls" -or $Mode -eq "both") { Write-Host "  RTMPS:    :$TLSPort" }
if ($EnableHLS) { Write-Host "  HLS:      enabled (hook on publish)" }
if ($EnableAuth) { Write-Host "  Auth:     token mode" }
Write-Host "  Log:      $LogFile"
Write-Host ""

if ($Foreground) {
    & $Binary @args_list 2>&1 | Tee-Object -FilePath $LogFile
} else {
    $proc = Start-Process -FilePath $Binary -ArgumentList $args_list `
        -RedirectStandardOutput $LogFile -RedirectStandardError (Join-Path $LogDir "rtmp-server-err.log") `
        -NoNewWindow -PassThru

    $proc.Id | Out-File -FilePath (Join-Path $ScriptDir ".server.pid") -Encoding ascii

    # Wait for server to be ready
    Write-Host "Waiting for server to start (PID: $($proc.Id))..." -ForegroundColor Yellow
    for ($i = 0; $i -lt 15; $i++) {
        Start-Sleep -Seconds 1
        if ($proc.HasExited) {
            Write-Host "ERROR: Server process exited unexpectedly." -ForegroundColor Red
            Get-Content $LogFile -ErrorAction SilentlyContinue
            exit 1
        }
        if (Test-Path $LogFile) {
            $content = Get-Content $LogFile -Raw -ErrorAction SilentlyContinue
            if ($content -and ($content -match "server listening|server started")) {
                Write-Host "Server started successfully." -ForegroundColor Green
                Write-Host "  PID: $($proc.Id)"
                Write-Host "  Stop with: Stop-Process -Id $($proc.Id)"
                exit 0
            }
        }
    }

    Write-Host "WARNING: Server may not be ready yet. Check $LogFile" -ForegroundColor Yellow
    exit 0
}
