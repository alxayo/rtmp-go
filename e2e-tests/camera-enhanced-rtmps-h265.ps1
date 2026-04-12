# ============================================================================
# TEST: camera-enhanced-rtmps-h265
# GROUP: Camera Tests
#
# WHAT IS TESTED:
#   Enhanced RTMP publish from a live camera using H.265 (HEVC) codec over
#   TLS (RTMPS). Combines Enhanced RTMP H.265 with TLS transport security.
#   The server records the stream to an MP4 file. Validates HEVC codec,
#   decodability, and TLS connection.
#
# EXPECTED RESULT:
#   - Camera H.265 stream published via Enhanced RTMPS (TLS)
#   - Server log confirms TLS connection
#   - Recording contains HEVC video codec and is decodable
#   - No server panics
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "camera-enhanced-rtmps-h265"
$Port = Get-UniquePort $TestName
$TlsPort = $Port + 100

# Skip if camera tests disabled
if ($env:SKIP_CAMERA_TESTS -eq "1") {
    Write-Host "SKIP: Camera tests disabled (SKIP_CAMERA_TESTS=1)" -ForegroundColor Yellow
    exit 2
}

# Check H.265 encoder availability
$codecs = & ffmpeg -codecs 2>&1
if ($codecs -notmatch "libx265") {
    Write-Host "SKIP: libx265 encoder not available" -ForegroundColor Yellow
    exit 2
}

# Check openssl is available
try {
    $null = & openssl version 2>&1
} catch {
    Write-Host "SKIP: openssl not found (required for cert generation)" -ForegroundColor Yellow
    exit 2
}

# Detect camera on Windows (dshow)
$devices = & ffmpeg -list_devices true -f dshow -i dummy 2>&1
if ($devices -notmatch "video") {
    Write-Host "SKIP: No camera device detected" -ForegroundColor Yellow
    exit 2
}

Setup $TestName

$recordDir = Join-Path $script:TmpDir "recordings"
New-Item -ItemType Directory -Path $recordDir -Force | Out-Null

# Generate self-signed certs
New-TestCerts

if (-not (Start-TestServer -Port $Port -ExtraArgs @(
    "-log-level", "debug",
    "-tls-listen", "localhost:${TlsPort}",
    "-tls-cert", (Join-Path $script:CertsDir "cert.pem"),
    "-tls-key", (Join-Path $script:CertsDir "key.pem"),
    "-record-all", "true",
    "-record-dir", $recordDir
))) { exit 1 }

$streamUrl = "rtmps://localhost:${TlsPort}/live/camera-secure-h265?tls_verify=0"

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing camera via Enhanced RTMPS H.265 (5s)..." -ForegroundColor Blue
$pubLog = Join-Path $script:LogDir "$TestName-publish.log"
$pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error",
    "-f", "dshow", "-framerate", "30", "-i", "video=0:audio=0",
    "-t", "5",
    "-c:v", "libx265", "-preset", "ultrafast",
    "-c:a", "aac", "-b:a", "128k",
    "-tls_verify", "0",
    "-f", "flv", $streamUrl
) -RedirectStandardOutput $pubLog -RedirectStandardError "$pubLog.err" -NoNewWindow -PassThru -Wait

Start-Sleep -Seconds 2

# Verify TLS connection in server log
Assert-LogContains -File $script:ServerLog -Pattern '"tls":true' -Label "TLS connection confirmed"

# Find recording (H.265 → MP4, fallback to FLV)
$recording = Get-ChildItem -Path $recordDir -Include "live_camera-secure-h265_*.mp4","live_camera-secure-h265_*.flv" -Recurse | Select-Object -First 1

if ($recording) {
    Assert-FileExists -File $recording.FullName -Label "Recording created"
    Assert-VideoCodec -File $recording.FullName -Expected "hevc"
    Assert-Decodable -File $recording.FullName
} else {
    Fail-Check "Recording created" "No recording file found in $recordDir"
}

Assert-LogNotContains -File $script:ServerLog -Pattern "panic|FATAL" -Label "No server panics"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
