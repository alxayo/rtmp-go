# ============================================================================
# TEST: srt-encrypt-wrong-passphrase
# GROUP: SRT Encryption
#
# WHAT IS TESTED:
#   SRT connection with a WRONG passphrase is rejected by the server.
#   The server uses one passphrase, FFmpeg uses a different one.
#
# EXPECTED RESULT:
#   - FFmpeg publish fails or server log shows handshake error
#   - No media is delivered
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "srt-encrypt-wrong-passphrase"
$Port = Get-UniquePort $TestName
$SrtPort = $Port + 200

$protocols = & ffmpeg -hide_banner -protocols 2>&1
if ($protocols -notmatch "srt") {
    Write-Host "SKIP: FFmpeg does not have SRT protocol support" -ForegroundColor Yellow
    exit 2
}

Setup $TestName
if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-srt-listen", "localhost:${SrtPort}", "-srt-passphrase", "server-secret-phrase", "-srt-pbkeylen", "16"))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing with WRONG passphrase (should be rejected)..." -ForegroundColor Blue
Publish-SrtH264 -Url "srt://localhost:${SrtPort}?streamid=publish:live/wrong-pass&latency=200000&passphrase=wrong-client-phrase&pbkeylen=16" -Duration 3
Start-Sleep -Seconds 2

# Check for rejection in server log
$logContent = if (Test-Path $script:ServerLog) { Get-Content $script:ServerLog -Raw } else { "" }
if ($logContent -match "unwrap SEK|wrong passphrase|handshake.*(fail|error)|encryption.*fail") {
    Pass-Check "Server rejected wrong passphrase"
} else {
    Fail-Check "Wrong passphrase rejection" "No rejection evidence found in server log"
}

Assert-LogNotContains -File $script:ServerLog -Pattern "panic|FATAL" -Label "No server panics"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
