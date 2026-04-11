# ============================================================================
# TEST: srt-publish-play-via-rtmp
# GROUP: SRT Ingest
#
# WHAT IS TESTED:
#   Cross-protocol: SRT publish → RTMP subscribe. Validates the SRT-to-RTMP
#   bridge pipeline.
#
# EXPECTED RESULT:
#   - RTMP subscriber captures valid H.264 video from SRT source
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "srt-publish-play-via-rtmp"
$Port = Get-UniquePort $TestName
$SrtPort = $Port + 200

$protocols = & ffmpeg -hide_banner -protocols 2>&1
if ($protocols -notmatch "srt") { Write-Host "SKIP: No SRT support" -ForegroundColor Yellow; exit 2 }

Setup $TestName
if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-srt-listen", "localhost:${SrtPort}"))) { exit 1 }

$capture = Join-Path $script:TmpDir "capture.flv"

# SRT publisher first (8s, background)
Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing H.264 via SRT (8s, background)..." -ForegroundColor Blue
Publish-SrtH264 -Url "srt://localhost:${SrtPort}?streamid=publish:live/srt-cross&latency=200000" -Duration 8 -Background

Start-Sleep -Seconds 2
Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Starting RTMP subscriber for SRT-bridged stream..." -ForegroundColor Blue
Start-Capture -Url "rtmp://localhost:${Port}/live/srt-cross" -Output $capture -Timeout 12

Start-Sleep -Seconds 8
Wait-AndStopCapture

Assert-FileExists -File $capture -Label "Cross-protocol capture exists"
Assert-VideoCodec -File $capture -Expected "h264"
Assert-Duration -File $capture -Min 2.0 -Max 12.0

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
