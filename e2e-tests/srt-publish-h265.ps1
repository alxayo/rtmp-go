# ============================================================================
# TEST: srt-publish-h265
# GROUP: SRT Ingest
#
# WHAT IS TESTED:
#   SRT ingest with H.265/HEVC via MPEG-TS. Tests server stability;
#   note H.265 SRT frames may be dropped (known limitation in bridge layer).
#
# EXPECTED RESULT:
#   - SRT connection accepted, no panics
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "srt-publish-h265"
$Port = Get-UniquePort $TestName
$SrtPort = $Port + 200

$protocols = & ffmpeg -hide_banner -protocols 2>&1
$encoders = & ffmpeg -hide_banner -encoders 2>&1
if ($protocols -notmatch "srt") { Write-Host "SKIP: No SRT support" -ForegroundColor Yellow; exit 2 }
if ($encoders -notmatch "libx265") { Write-Host "SKIP: No libx265" -ForegroundColor Yellow; exit 2 }

Setup $TestName
if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-srt-listen", "localhost:${SrtPort}"))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing H.265 via SRT (5s)..." -ForegroundColor Blue
Publish-SrtH265 -Url "srt://localhost:${SrtPort}?streamid=publish:live/srt-h265&latency=200000" -Duration 5
Start-Sleep -Seconds 2

Assert-LogNotContains -File $script:ServerLog -Pattern "panic|FATAL" -Label "No server panics"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
