# ============================================================================
# TEST: hooks-hls-conversion
# GROUP: Event Hooks
#
# WHAT IS TESTED:
#   HLS output: .m3u8 playlist + .ts segments are generated during publish.
#   Skips if server doesn't support HLS flags.
#
# EXPECTED RESULT:
#   - .m3u8 with #EXTM3U header, at least 1 .ts segment
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "hooks-hls-conversion"
$Port = Get-UniquePort $TestName

Setup $TestName

$hlsDir = Join-Path $script:TmpDir "hls-output"
New-Item -ItemType Directory -Path $hlsDir -Force | Out-Null

# Check HLS support
$helpOutput = & $script:Binary -help 2>&1
if ($helpOutput -notmatch "hls") {
    Write-Host "SKIP: Server does not support HLS output" -ForegroundColor Yellow
    Teardown
    exit 2
}

if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-hls", "true", "-hls-dir", $hlsDir))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing for HLS (8s)..." -ForegroundColor Blue
Publish-TestPattern -Url "rtmp://localhost:${Port}/live/hls-test" -Duration 8
Start-Sleep -Seconds 5

$m3u8 = Get-ChildItem -Path $hlsDir -Filter "*.m3u8" -Recurse | Select-Object -First 1
$tsCount = (Get-ChildItem -Path $hlsDir -Filter "*.ts" -Recurse).Count

if ($m3u8) {
    Pass-Check "HLS playlist created: $($m3u8.Name)"
    $content = Get-Content $m3u8.FullName -Raw
    if ($content -match "#EXTM3U") { Pass-Check "Playlist has #EXTM3U header" }
    else { Fail-Check "Playlist format" "Missing #EXTM3U header" }
} else {
    Fail-Check "HLS playlist created" "No .m3u8 file found"
}

if ($tsCount -gt 0) { Pass-Check "HLS segments created ($tsCount .ts files)" }
else { Fail-Check "HLS segments created" "No .ts files found" }

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
