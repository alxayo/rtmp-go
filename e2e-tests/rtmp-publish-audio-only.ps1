# ============================================================================
# TEST: rtmp-publish-audio-only
# GROUP: RTMP Basic
#
# WHAT IS TESTED:
#   Audio-only RTMP stream (AAC, no video). Verifies the server handles
#   streams without a video component.
#
# EXPECTED RESULT:
#   - Server accepts audio-only publish, no panics or errors
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "rtmp-publish-audio-only"
$Port = Get-UniquePort $TestName

Setup $TestName
if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug"))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing audio-only stream (5s)..." -ForegroundColor Blue
$logFile = Join-Path $script:LogDir "$TestName-publish-audio.log"
$proc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error",
    "-f", "lavfi", "-i", "sine=frequency=440:duration=5",
    "-c:a", "aac", "-b:a", "64k",
    "-f", "flv", "rtmp://localhost:${Port}/live/audio"
) -RedirectStandardOutput $logFile -RedirectStandardError "$logFile.err" -NoNewWindow -PassThru -Wait

Start-Sleep -Seconds 2
Assert-LogContains -File $script:ServerLog -Pattern "connection registered" -Label "Server registered the connection"
Assert-LogNotContains -File $script:ServerLog -Pattern "panic|FATAL" -Label "No server panics"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
