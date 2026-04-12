# ============================================================================
# TEST: camera-rtmps-h264
# GROUP: Camera Tests
#
# WHAT IS TESTED:
#   Secure RTMP (RTMPS/TLS) publish from a live camera device using H.264.
#   The server is configured with TLS certificates. FFmpeg publishes the
#   camera feed over an encrypted RTMPS connection.
#
# EXPECTED RESULT:
#   - Camera stream published via RTMPS; TLS handshake succeeds; no panics
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "camera-rtmps-h264"
$Port = Get-UniquePort $TestName
$TlsPort = $Port + 100

# Allow skipping camera tests via environment variable
if ($env:SKIP_CAMERA_TESTS -eq "1") {
    Write-Host "SKIP: Camera tests disabled (set SKIP_CAMERA_TESTS=0 to enable)" -ForegroundColor Yellow
    exit 2
}

# Detect camera on Windows (dshow)
$devices = & ffmpeg -list_devices true -f dshow -i dummy 2>&1
if ($devices -notmatch "video") {
    Write-Host "SKIP: No camera device detected" -ForegroundColor Yellow
    exit 2
}

# Check openssl is available
$opensslCheck = Get-Command openssl -ErrorAction SilentlyContinue
if (-not $opensslCheck) {
    Write-Host "SKIP: openssl not found (required for TLS certs)" -ForegroundColor Yellow
    exit 2
}

Setup $TestName
Generate-Certs

if (-not (Start-TestServer -Port $Port -ExtraArgs @(
    "-log-level", "debug",
    "-tls-listen", "localhost:${TlsPort}",
    "-tls-cert", (Join-Path $script:CertsDir "server.crt"),
    "-tls-key", (Join-Path $script:CertsDir "server.key")
))) { exit 1 }

$StreamUrl = "rtmps://localhost:${TlsPort}/live/camera-secure"

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing camera via RTMPS (5s)..." -ForegroundColor Blue
$log = Join-Path $script:LogDir "$TestName-camera.log"
$proc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error",
    "-f", "dshow", "-framerate", "30", "-i", "video=0:audio=0",
    "-t", "5",
    "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
    "-c:a", "aac", "-b:a", "128k",
    "-tls_verify", "0",
    "-f", "flv", $StreamUrl
) -RedirectStandardOutput $log -RedirectStandardError "$log.err" -NoNewWindow -PassThru -Wait

Start-Sleep -Seconds 2

Assert-LogContains -File $script:ServerLog -Pattern "tls.*true" -Label "TLS connection established"
Assert-LogNotContains -File $script:ServerLog -Pattern "panic|FATAL" -Label "No server panics"
Pass-Check "Camera stream published over RTMPS"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
