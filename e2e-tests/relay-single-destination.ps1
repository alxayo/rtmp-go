# ============================================================================
# TEST: relay-single-destination
# GROUP: Relay
#
# WHAT IS TESTED:
#   Media relay from primary server to a single destination server.
#   Subscriber on the destination captures the relayed stream.
#
# EXPECTED RESULT:
#   - Relay capture on destination has valid H.264 video
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "relay-single-destination"
$Port = Get-UniquePort $TestName
$DestPort = $Port + 1

Setup $TestName
Build-Server

# Start destination server
$destLog = Join-Path $script:LogDir "relay-dest-${DestPort}.log"
$destProc = Start-Process -FilePath $script:Binary -ArgumentList @(
    "-listen", "localhost:${DestPort}", "-log-level", "debug"
) -RedirectStandardOutput $destLog -RedirectStandardError "$destLog.err" -NoNewWindow -PassThru
Start-Sleep -Seconds 1

if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-relay-to", "rtmp://localhost:${DestPort}/live"))) { exit 1 }

# Publish first (8s, background)
Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing to primary (8s, background)..." -ForegroundColor Blue
$pubLog = Join-Path $script:LogDir "$TestName-pub.log"
$pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error", "-re",
    "-f", "lavfi", "-i", "testsrc=duration=8:size=320x240:rate=25",
    "-f", "lavfi", "-i", "sine=frequency=440:duration=8",
    "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
    "-c:a", "aac", "-b:a", "64k",
    "-f", "flv", "rtmp://localhost:${Port}/live/relay-test"
) -RedirectStandardOutput $pubLog -RedirectStandardError "$pubLog.err" -NoNewWindow -PassThru

Start-Sleep -Seconds 2
$capture = Join-Path $script:TmpDir "relay-capture.flv"
Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Starting subscriber on destination..." -ForegroundColor Blue
Start-Capture -Url "rtmp://localhost:${DestPort}/live/relay-test" -Output $capture -Timeout 12

$pubProc.WaitForExit()
Start-Sleep -Seconds 2
Wait-AndStopCapture

if (-not $destProc.HasExited) { $destProc.Kill() }

Assert-FileExists -File $capture -Label "Relay capture exists on destination"
Assert-VideoCodec -File $capture -Expected "h264"
Assert-Duration -File $capture -Min 2.0 -Max 12.0

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
