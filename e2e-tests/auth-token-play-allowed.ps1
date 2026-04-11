# ============================================================================
# TEST: auth-token-play-allowed
# GROUP: Authentication
#
# WHAT IS TESTED:
#   Subscribing with the correct token succeeds and captures valid video.
#
# EXPECTED RESULT:
#   - Subscriber capture exists with valid video
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "auth-token-play-allowed"
$Port = Get-UniquePort $TestName

Setup $TestName
if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-auth-mode", "token", "-auth-token", "live/auth-play=secret123"))) { exit 1 }

$capture = Join-Path $script:TmpDir "capture.flv"

# Publish first (8s, background)
Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing with correct token (8s, background)..." -ForegroundColor Blue
$pubLog = Join-Path $script:LogDir "$TestName-pub.log"
$pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error", "-re",
    "-f", "lavfi", "-i", "testsrc=duration=8:size=320x240:rate=25",
    "-f", "lavfi", "-i", "sine=frequency=440:duration=8",
    "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
    "-c:a", "aac", "-b:a", "64k",
    "-f", "flv", "rtmp://localhost:${Port}/live/auth-play?token=secret123"
) -RedirectStandardOutput $pubLog -RedirectStandardError "$pubLog.err" -NoNewWindow -PassThru

Start-Sleep -Seconds 2
Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Starting authorized subscriber..." -ForegroundColor Blue
Start-Capture -Url "rtmp://localhost:${Port}/live/auth-play?token=secret123" -Output $capture -Timeout 10

$pubProc.WaitForExit()
Start-Sleep -Seconds 2
Wait-AndStopCapture

Assert-FileExists -File $capture -Label "Authorized subscriber capture exists"
Assert-HasVideo -File $capture
Assert-LogNotContains -File $script:ServerLog -Pattern "auth_failed|rejected" -Label "No auth failures"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
