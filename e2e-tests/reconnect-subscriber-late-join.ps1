# ============================================================================
# TEST: reconnect-subscriber-late-join
# GROUP: Connection Lifecycle
#
# WHAT IS TESTED:
#   Late-joining subscriber receives cached headers and decodes the stream.
#
# EXPECTED RESULT:
#   - Late-join capture has valid, decodable H.264 video
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "reconnect-subscriber-late-join"
$Port = Get-UniquePort $TestName

Setup $TestName
if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug"))) { exit 1 }

$url = "rtmp://localhost:${Port}/live/late-join"

# Start publisher (8s)
$pubLog = Join-Path $script:LogDir "$TestName-pub.log"
$pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error", "-re",
    "-f", "lavfi", "-i", "testsrc=duration=8:size=320x240:rate=25",
    "-f", "lavfi", "-i", "sine=frequency=440:duration=8",
    "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
    "-c:a", "aac", "-b:a", "64k", "-f", "flv", $url
) -RedirectStandardOutput $pubLog -RedirectStandardError "$pubLog.err" -NoNewWindow -PassThru

# Late join after 3s
Start-Sleep -Seconds 3
$capture = Join-Path $script:TmpDir "late-join-capture.flv"
Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Late-joining subscriber..." -ForegroundColor Blue
Start-Capture -Url $url -Output $capture -Timeout 8

$pubProc.WaitForExit()
Start-Sleep -Seconds 2
Wait-AndStopCapture

Assert-FileExists -File $capture -Label "Late-join capture exists"
Assert-VideoCodec -File $capture -Expected "h264"
Assert-Decodable -File $capture

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
