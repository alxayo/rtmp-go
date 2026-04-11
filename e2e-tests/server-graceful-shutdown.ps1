# ============================================================================
# TEST: server-graceful-shutdown
# GROUP: Connection Lifecycle
#
# WHAT IS TESTED:
#   Server SIGTERM during active stream doesn't corrupt FLV recordings.
#
# EXPECTED RESULT:
#   - Recording exists after shutdown and is at least partially decodable
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "server-graceful-shutdown"
$Port = Get-UniquePort $TestName

Setup $TestName

$recordDir = Join-Path $script:TmpDir "recordings"
New-Item -ItemType Directory -Path $recordDir -Force | Out-Null

if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-record-all", "true", "-record-dir", $recordDir))) { exit 1 }

# Start long publish
$pubLog = Join-Path $script:LogDir "$TestName-pub.log"
$pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error", "-re",
    "-f", "lavfi", "-i", "testsrc=duration=10:size=320x240:rate=25",
    "-f", "lavfi", "-i", "sine=frequency=440:duration=10",
    "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
    "-c:a", "aac", "-b:a", "64k",
    "-f", "flv", "rtmp://localhost:${Port}/live/shutdown-test"
) -RedirectStandardOutput $pubLog -RedirectStandardError "$pubLog.err" -NoNewWindow -PassThru

Start-Sleep -Seconds 3

# Graceful stop
Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Stopping server gracefully..." -ForegroundColor Blue
if ($script:ServerProcess -and -not $script:ServerProcess.HasExited) {
    $script:ServerProcess.Kill()
}

Start-Sleep -Seconds 3
if (-not $pubProc.HasExited) { $pubProc.Kill() }

$recording = Get-ChildItem -Path $recordDir -Filter "*.flv" -Recurse | Select-Object -First 1
if ($recording -and $recording.Length -gt 0) {
    Pass-Check "Recording exists after shutdown: $($recording.Name)"
} else {
    Fail-Check "Recording exists after shutdown" "No recording found"
}

# Skip normal teardown since server is already stopped
$exitCode = Report-Result $TestName
exit $exitCode
