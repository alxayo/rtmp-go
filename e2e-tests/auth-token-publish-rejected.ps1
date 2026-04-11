# ============================================================================
# TEST: auth-token-publish-rejected
# GROUP: Authentication
#
# WHAT IS TESTED:
#   Publishing with WRONG token is rejected by the server.
#
# EXPECTED RESULT:
#   - Server rejects publish, log shows auth failure
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "auth-token-publish-rejected"
$Port = Get-UniquePort $TestName

Setup $TestName
if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-auth-mode", "token", "-auth-token", "live/auth-test=secret123"))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing with WRONG token (should be rejected)..." -ForegroundColor Blue
$logFile = Join-Path $script:LogDir "$TestName-badpub.log"
$proc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error", "-re",
    "-f", "lavfi", "-i", "testsrc=duration=3:size=320x240:rate=25",
    "-f", "lavfi", "-i", "sine=frequency=440:duration=3",
    "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
    "-c:a", "aac", "-b:a", "64k",
    "-f", "flv", "rtmp://localhost:${Port}/live/auth-test?token=wrongtoken"
) -RedirectStandardOutput $logFile -RedirectStandardError "$logFile.err" -NoNewWindow -PassThru -Wait

Start-Sleep -Seconds 2

$logContent = if (Test-Path $script:ServerLog) { Get-Content $script:ServerLog -Raw } else { "" }
if ($logContent -match "auth|reject|denied|failed" -or $proc.ExitCode -ne 0) {
    Pass-Check "Server rejected unauthorized publish"
} else {
    Fail-Check "Auth rejection" "Publish appeared to succeed with wrong token"
}

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
