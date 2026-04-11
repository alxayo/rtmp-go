# ============================================================================
# TEST: rtmp-concurrent-publishers
# GROUP: RTMP Basic
#
# WHAT IS TESTED:
#   Two simultaneous publishers on different stream keys. Verifies the server
#   handles concurrent publish sessions without interference.
#
# EXPECTED RESULT:
#   - Both publishers accepted, 2+ connections in server log
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "rtmp-concurrent-publishers"
$Port = Get-UniquePort $TestName

Setup $TestName
if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug"))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Starting 2 concurrent publishers..." -ForegroundColor Blue
$log1 = Join-Path $script:LogDir "$TestName-pub1.log"
$log2 = Join-Path $script:LogDir "$TestName-pub2.log"

$p1 = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error", "-re",
    "-f", "lavfi", "-i", "testsrc=duration=5:size=320x240:rate=25",
    "-f", "lavfi", "-i", "sine=frequency=440:duration=5",
    "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
    "-c:a", "aac", "-b:a", "64k", "-f", "flv", "rtmp://localhost:${Port}/live/stream-a"
) -RedirectStandardOutput $log1 -RedirectStandardError "$log1.err" -NoNewWindow -PassThru

$p2 = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error", "-re",
    "-f", "lavfi", "-i", "testsrc=duration=5:size=320x240:rate=25",
    "-f", "lavfi", "-i", "sine=frequency=440:duration=5",
    "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
    "-c:a", "aac", "-b:a", "64k", "-f", "flv", "rtmp://localhost:${Port}/live/stream-b"
) -RedirectStandardOutput $log2 -RedirectStandardError "$log2.err" -NoNewWindow -PassThru

$p1.WaitForExit()
$p2.WaitForExit()
Start-Sleep -Seconds 2

$count = (Select-String -Path $script:ServerLog -Pattern "connection registered" -ErrorAction SilentlyContinue).Count
if ($count -ge 2) { Pass-Check "Both publishers registered ($count connections)" }
else { Fail-Check "Both publishers registered" "only $count connections found" }

Assert-LogNotContains -File $script:ServerLog -Pattern "panic|FATAL" -Label "No server panics"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
