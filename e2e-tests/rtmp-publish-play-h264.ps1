# ============================================================================
# TEST: rtmp-publish-play-h264
# GROUP: RTMP Basic
#
# WHAT IS TESTED:
#   Full RTMP publish-to-subscribe cycle using H.264 video + AAC audio.
#   Validates the complete pub/sub pipeline.
#
# EXPECTED RESULT:
#   - Captured file contains H.264 video and AAC audio, duration >= 2s
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "rtmp-publish-play-h264"
$Port = Get-UniquePort $TestName

Setup $TestName

if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug"))) { exit 1 }

$capture = Join-Path $script:TmpDir "capture.flv"
Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Starting publisher in background (8s)..." -ForegroundColor Blue
$pubLog = Join-Path $script:LogDir "$TestName-pub.log"
$pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error", "-re",
    "-f", "lavfi", "-i", "testsrc=duration=8:size=320x240:rate=25",
    "-f", "lavfi", "-i", "sine=frequency=440:duration=8",
    "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
    "-c:a", "aac", "-b:a", "64k", "-f", "flv", "rtmp://localhost:${Port}/live/test"
) -RedirectStandardOutput $pubLog -RedirectStandardError "$pubLog.err" -NoNewWindow -PassThru

Start-Sleep -Seconds 2
Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Starting subscriber capture..." -ForegroundColor Blue
$capProc = Start-Capture -Url "rtmp://localhost:${Port}/live/test" -Output $capture -Timeout 10

$pubProc.WaitForExit()
Start-Sleep -Seconds 2
Wait-AndStopCapture

Assert-FileExists -File $capture -Label "Capture file exists"
Assert-VideoCodec -File $capture -Expected "h264"
Assert-AudioCodec -File $capture -Expected "aac"
Assert-Duration -File $capture -Min 2.0 -Max 10.0

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
