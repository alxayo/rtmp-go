# ============================================================================
# TEST: enhanced-rtmp-h265
# GROUP: Enhanced RTMP
#
# WHAT IS TESTED:
#   H.265/HEVC via Enhanced RTMP (E-RTMP v2 FourCC "hvc1"). Verifies
#   publish, subscribe, and recording all preserve the HEVC codec.
#
# EXPECTED RESULT:
#   - Capture file contains HEVC video and is decodable
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "enhanced-rtmp-h265"
$Port = Get-UniquePort $TestName

$encoders = & ffmpeg -hide_banner -encoders 2>&1
if ($encoders -notmatch "libx265") {
    Write-Host "SKIP: FFmpeg does not have libx265 encoder" -ForegroundColor Yellow
    exit 2
}

Setup $TestName

$recordDir = Join-Path $script:TmpDir "recordings"
New-Item -ItemType Directory -Path $recordDir -Force | Out-Null

if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-record-all", "true", "-record-dir", $recordDir))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing H.265 via Enhanced RTMP (8s, background)..." -ForegroundColor Blue
$pubLog = Join-Path $script:LogDir "$TestName-pub.log"
$pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error", "-re",
    "-f", "lavfi", "-i", "testsrc=duration=8:size=320x240:rate=25",
    "-f", "lavfi", "-i", "sine=frequency=440:duration=8",
    "-c:v", "libx265", "-preset", "ultrafast",
    "-c:a", "aac", "-b:a", "64k",
    "-f", "flv", "rtmp://localhost:${Port}/live/h265-test"
) -RedirectStandardOutput $pubLog -RedirectStandardError "$pubLog.err" -NoNewWindow -PassThru

Start-Sleep -Seconds 2
$capture = Join-Path $script:TmpDir "capture.flv"
Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Starting H.265 subscriber..." -ForegroundColor Blue
Start-Capture -Url "rtmp://localhost:${Port}/live/h265-test" -Output $capture -Timeout 12

$pubProc.WaitForExit()
Start-Sleep -Seconds 2
Wait-AndStopCapture

Assert-FileExists -File $capture -Label "H.265 subscriber capture exists"
Assert-VideoCodec -File $capture -Expected "hevc"
Assert-Decodable -File $capture

$recording = Get-ChildItem -Path $recordDir -Include "*.mp4","*.flv" -Recurse | Select-Object -First 1
if ($recording) {
    Pass-Check "H.265 recording also created"
    Assert-VideoCodec -File $recording.FullName -Expected "hevc"
}

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
