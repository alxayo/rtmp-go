# ============================================================================
# TEST: auth-token-play-rejected
# GROUP: Authentication
#
# WHAT IS TESTED:
#   Subscribing with the WRONG token is rejected.
#
# EXPECTED RESULT:
#   - Subscriber capture is empty/missing, server log shows rejection
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "auth-token-play-rejected"
$Port = Get-UniquePort $TestName

Setup $TestName
if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-auth-mode", "token", "-auth-token", "live/auth-play-rej=secret123"))) { exit 1 }

# Publish with correct token in background
$pubLog = Join-Path $script:LogDir "$TestName-pub.log"
$pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error", "-re",
    "-f", "lavfi", "-i", "testsrc=duration=5:size=320x240:rate=25",
    "-f", "lavfi", "-i", "sine=frequency=440:duration=5",
    "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
    "-c:a", "aac", "-b:a", "64k",
    "-f", "flv", "rtmp://localhost:${Port}/live/auth-play-rej?token=secret123"
) -RedirectStandardOutput $pubLog -RedirectStandardError "$pubLog.err" -NoNewWindow -PassThru

Start-Sleep -Seconds 1

# Try subscriber with wrong token
$capture = Join-Path $script:TmpDir "capture.flv"
$subLog = Join-Path $script:LogDir "$TestName-sub.log"
$subProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error", "-re",
    "-i", "rtmp://localhost:${Port}/live/auth-play-rej?token=badtoken",
    "-c", "copy", "-t", "5", $capture
) -RedirectStandardOutput $subLog -RedirectStandardError "$subLog.err" -NoNewWindow -PassThru -Wait

$pubProc.WaitForExit()
Start-Sleep -Seconds 1

if (-not (Test-Path $capture) -or (Get-Item $capture -ErrorAction SilentlyContinue).Length -eq 0) {
    Pass-Check "Unauthorized subscriber got no data"
} elseif ($subProc.ExitCode -ne 0) {
    Pass-Check "Subscriber failed with exit code $($subProc.ExitCode)"
} else {
    Fail-Check "Auth rejection for play" "Subscriber captured data with wrong token"
}

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
