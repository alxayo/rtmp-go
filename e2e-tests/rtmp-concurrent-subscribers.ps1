# ============================================================================
# TEST: rtmp-concurrent-subscribers
# GROUP: RTMP Basic
#
# WHAT IS TESTED:
#   1 publisher → 3 concurrent subscribers. Validates fan-out broadcast.
#
# EXPECTED RESULT:
#   - All 3 capture files exist with valid video
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "rtmp-concurrent-subscribers"
$Port = Get-UniquePort $TestName

Setup $TestName
if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug"))) { exit 1 }

$url = "rtmp://localhost:${Port}/live/fanout"
$cap1 = Join-Path $script:TmpDir "sub1.flv"
$cap2 = Join-Path $script:TmpDir "sub2.flv"
$cap3 = Join-Path $script:TmpDir "sub3.flv"

# Start publisher first (10s)
Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Starting publisher (10s)..." -ForegroundColor Blue
$pubLog = Join-Path $script:LogDir "$TestName-pub.log"
$pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error", "-re",
    "-f", "lavfi", "-i", "testsrc=duration=10:size=320x240:rate=25",
    "-f", "lavfi", "-i", "sine=frequency=440:duration=10",
    "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
    "-c:a", "aac", "-b:a", "64k", "-f", "flv", $url
) -RedirectStandardOutput $pubLog -RedirectStandardError "$pubLog.err" -NoNewWindow -PassThru

Start-Sleep -Seconds 2
Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Starting 3 subscribers..." -ForegroundColor Blue
$s1 = Start-Capture -Url $url -Output $cap1 -Timeout 10
$s1Pid = $script:CapturePid
$s2 = Start-Capture -Url $url -Output $cap2 -Timeout 10
$s2Pid = $script:CapturePid
$s3 = Start-Capture -Url $url -Output $cap3 -Timeout 10
$s3Pid = $script:CapturePid

$pubProc.WaitForExit()
Start-Sleep -Seconds 2
Wait-AndStopCapture -Pid $s1Pid
Wait-AndStopCapture -Pid $s2Pid
Wait-AndStopCapture -Pid $s3Pid

Assert-FileExists -File $cap1 -Label "Subscriber 1 capture exists"
Assert-FileExists -File $cap2 -Label "Subscriber 2 capture exists"
Assert-FileExists -File $cap3 -Label "Subscriber 3 capture exists"
Assert-HasVideo -File $cap1

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
