# ============================================================================
# TEST: rtmp-publish-h264
# GROUP: RTMP Basic
#
# WHAT IS TESTED:
#   Basic RTMP publish with H.264 video + AAC audio. Verifies the server
#   accepts an inbound RTMP connection from FFmpeg and registers the
#   publisher without errors.
#
# EXPECTED RESULT:
#   - Server starts and accepts the connection
#   - Server log shows "connection registered" and publish activity
#   - No errors or crashes during the publish session
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "rtmp-publish-h264"
$Port = Get-UniquePort $TestName

Setup $TestName

if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug"))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing H.264+AAC test pattern (5s)..." -ForegroundColor Blue
Publish-TestPattern -Url "rtmp://localhost:${Port}/live/test" -Duration 5
Start-Sleep -Seconds 2

Assert-LogContains -File $script:ServerLog -Pattern "connection registered" -Label "Server registered the connection"
Assert-LogNotContains -File $script:ServerLog -Pattern "panic|FATAL|fatal error" -Label "No server panics"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
