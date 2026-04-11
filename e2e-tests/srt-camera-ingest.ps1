# ============================================================================
# TEST: srt-camera-ingest
# GROUP: SRT Ingest
#
# WHAT IS TESTED:
#   SRT ingest from system camera. Auto-skips if no camera detected.
#
# EXPECTED RESULT:
#   - Camera available: SRT connection accepted; no camera: SKIP (exit 2)
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "srt-camera-ingest"
$Port = Get-UniquePort $TestName
$SrtPort = $Port + 200

$protocols = & ffmpeg -hide_banner -protocols 2>&1
if ($protocols -notmatch "srt") { Write-Host "SKIP: No SRT support" -ForegroundColor Yellow; exit 2 }

# Detect camera on Windows (dshow)
$devices = & ffmpeg -list_devices true -f dshow -i dummy 2>&1
if ($devices -notmatch "video") {
    Write-Host "SKIP: No camera device detected" -ForegroundColor Yellow
    exit 2
}

Setup $TestName
if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-srt-listen", "localhost:${SrtPort}"))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing camera feed via SRT (3s)..." -ForegroundColor Blue
$log = Join-Path $script:LogDir "$TestName-camera.log"
$proc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error",
    "-f", "dshow", "-framerate", "30", "-i", "video=0",
    "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
    "-t", "3", "-f", "mpegts",
    "srt://localhost:${SrtPort}?streamid=publish:live/camera&latency=200000"
) -RedirectStandardOutput $log -RedirectStandardError "$log.err" -NoNewWindow -PassThru -Wait

Start-Sleep -Seconds 2
Assert-LogNotContains -File $script:ServerLog -Pattern "panic|FATAL" -Label "No server panics"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
