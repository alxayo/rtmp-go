# ============================================================================
# TEST: reconnect-publisher-disconnect
# GROUP: Connection Lifecycle
#
# WHAT IS TESTED:
#   Subscriber handles publisher disconnect gracefully.
#
# EXPECTED RESULT:
#   - Subscriber exits cleanly, no server panics
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "reconnect-publisher-disconnect"
$Port = Get-UniquePort $TestName

Setup $TestName
if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug"))) { exit 1 }

$url = "rtmp://localhost:${Port}/live/disconnect-test"
$capture = Join-Path $script:TmpDir "capture.flv"

# Publisher first (3s, background)
Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing 3s then disconnecting..." -ForegroundColor Blue
$pubLog = Join-Path $script:LogDir "$TestName-pub.log"
$pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error", "-re",
    "-f", "lavfi", "-i", "testsrc=duration=3:size=320x240:rate=25",
    "-f", "lavfi", "-i", "sine=frequency=440:duration=3",
    "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
    "-c:a", "aac", "-b:a", "64k", "-f", "flv", $url
) -RedirectStandardOutput $pubLog -RedirectStandardError "$pubLog.err" -NoNewWindow -PassThru

Start-Sleep -Seconds 1
Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Starting subscriber..." -ForegroundColor Blue
Start-Capture -Url $url -Output $capture -Timeout 15

$pubProc.WaitForExit()
Start-Sleep -Seconds 5
Wait-AndStopCapture

Assert-LogNotContains -File $script:ServerLog -Pattern "panic|FATAL" -Label "No server panics"
Pass-Check "Server handled publisher disconnect without panic"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
