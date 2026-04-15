# ============================================================================
# TEST: srt-encrypt-no-encryption-rejected
# GROUP: SRT Encryption
#
# WHAT IS TESTED:
#   When the server requires SRT encryption, an unencrypted client (no
#   passphrase) should be rejected. The server expects a KMREQ extension;
#   without it, the handshake fails.
#
# EXPECTED RESULT:
#   - FFmpeg publish fails or server log shows "encryption required"
#   - No media is delivered
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "srt-encrypt-no-encryption-rejected"
$Port = Get-UniquePort $TestName
$SrtPort = $Port + 200

$protocols = & ffmpeg -hide_banner -protocols 2>&1
if ($protocols -notmatch "srt") {
    Write-Host "SKIP: FFmpeg does not have SRT protocol support" -ForegroundColor Yellow
    exit 2
}

Setup $TestName
if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-srt-listen", "localhost:${SrtPort}", "-srt-passphrase", "server-secret-phrase", "-srt-pbkeylen", "16"))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing WITHOUT encryption (should be rejected)..." -ForegroundColor Blue
Publish-SrtH264 -Url "srt://localhost:${SrtPort}?streamid=publish:live/no-encrypt&latency=200000" -Duration 3
Start-Sleep -Seconds 2

# Check for rejection in server log
$logContent = if (Test-Path $script:ServerLog) { Get-Content $script:ServerLog -Raw } else { "" }
if ($logContent -match "encryption required|no KMREQ|handshake.*(fail|error)") {
    Pass-Check "Server rejected unencrypted client"
} else {
    Fail-Check "No-encryption rejection" "No rejection evidence found in server log"
}

Assert-LogNotContains -File $script:ServerLog -Pattern "panic|FATAL" -Label "No server panics"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
