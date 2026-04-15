# ============================================================================
# TEST: srt-encrypt-publish-play-via-rtmp
# GROUP: SRT Encryption
#
# WHAT IS TESTED:
#   Cross-protocol streaming with encryption: encrypted SRT publish → RTMP
#   subscriber. Validates that SRT decryption is transparent to the RTMP
#   bridge pipeline.
#
# EXPECTED RESULT:
#   - Encrypted SRT publisher connects, RTMP subscriber captures valid video
#   - Capture duration >= 2 seconds
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "srt-encrypt-publish-play-via-rtmp"
$Port = Get-UniquePort $TestName
$SrtPort = $Port + 200

$protocols = & ffmpeg -hide_banner -protocols 2>&1
if ($protocols -notmatch "srt") { Write-Host "SKIP: No SRT support" -ForegroundColor Yellow; exit 2 }

Setup $TestName
if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-srt-listen", "localhost:${SrtPort}", "-srt-passphrase", "test-secret-passphrase", "-srt-pbkeylen", "16"))) { exit 1 }

$capture = Join-Path $script:TmpDir "capture.flv"

# Encrypted SRT publisher first (10s, background)
Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing H.264 via encrypted SRT (10s, background)..." -ForegroundColor Blue
Publish-SrtH264 -Url "srt://localhost:${SrtPort}?streamid=publish:live/encrypt-cross&latency=200000&passphrase=test-secret-passphrase&pbkeylen=16" -Duration 10 -Background

# Wait for encrypted SRT handshake to complete and publish session to register
if (-not (Wait-ForLog -File $script:ServerLog -Pattern "publish session started" -Timeout 10)) {
    Write-Host "ERROR: SRT publisher did not register within 10s" -ForegroundColor Red
    Teardown
    exit 1
}
Start-Sleep -Seconds 1
Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Starting RTMP subscriber for encrypted SRT-bridged stream..." -ForegroundColor Blue
Start-Capture -Url "rtmp://localhost:${Port}/live/encrypt-cross" -Output $capture -Timeout 12

Start-Sleep -Seconds 8
Wait-AndStopCapture

Assert-FileExists -File $capture -Label "Cross-protocol encrypted capture exists"
Assert-VideoCodec -File $capture -Expected "h264"
Assert-Duration -File $capture -Min 2.0 -Max 12.0
Assert-LogContains -File $script:ServerLog -Pattern "SRT encryption enabled" -Label "Encryption is enabled"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
