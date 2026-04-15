# ============================================================================
# TEST: srt-encrypt-publish-aes256
# GROUP: SRT Encryption
#
# WHAT IS TESTED:
#   SRT ingest with AES-256 encryption (strongest key size). The server is
#   started with -srt-passphrase and -srt-pbkeylen 32 (AES-256). FFmpeg
#   publishes with matching passphrase and pbkeylen=32.
#
# EXPECTED RESULT:
#   - Server log shows "SRT encryption enabled" with "AES-256"
#   - No panics or encryption failures
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "srt-encrypt-publish-aes256"
$Port = Get-UniquePort $TestName
$SrtPort = $Port + 200

$protocols = & ffmpeg -hide_banner -protocols 2>&1
if ($protocols -notmatch "srt") {
    Write-Host "SKIP: FFmpeg does not have SRT protocol support" -ForegroundColor Yellow
    exit 2
}

Setup $TestName
if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-srt-listen", "localhost:${SrtPort}", "-srt-passphrase", "test-secret-passphrase", "-srt-pbkeylen", "32"))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing H.264+AAC via encrypted SRT (AES-256, 5s)..." -ForegroundColor Blue
Publish-SrtH264 -Url "srt://localhost:${SrtPort}?streamid=publish:live/encrypt-aes256&latency=200000&passphrase=test-secret-passphrase&pbkeylen=32" -Duration 5
Start-Sleep -Seconds 2

Assert-LogContains -File $script:ServerLog -Pattern "SRT encryption enabled" -Label "Encryption is enabled"
Assert-LogContains -File $script:ServerLog -Pattern "AES-256" -Label "Using AES-256 key length"
Assert-LogNotContains -File $script:ServerLog -Pattern "panic|FATAL" -Label "No server panics"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
