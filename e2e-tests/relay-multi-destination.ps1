# ============================================================================
# TEST: relay-multi-destination
# GROUP: Relay
#
# WHAT IS TESTED:
#   Media relay to 2 destination servers. Primary fans out to both.
#
# EXPECTED RESULT:
#   - Both destinations receive valid H.264 video
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "relay-multi-destination"
$Port = Get-UniquePort $TestName
$Dest1Port = $Port + 1
$Dest2Port = $Port + 2

Setup $TestName
Build-Server

$dest1Log = Join-Path $script:LogDir "relay-dest1.log"
$dest2Log = Join-Path $script:LogDir "relay-dest2.log"

$d1 = Start-Process -FilePath $script:Binary -ArgumentList @("-listen", "localhost:${Dest1Port}", "-log-level", "debug") -RedirectStandardOutput $dest1Log -RedirectStandardError "$dest1Log.err" -NoNewWindow -PassThru
$d2 = Start-Process -FilePath $script:Binary -ArgumentList @("-listen", "localhost:${Dest2Port}", "-log-level", "debug") -RedirectStandardOutput $dest2Log -RedirectStandardError "$dest2Log.err" -NoNewWindow -PassThru
Start-Sleep -Seconds 1

if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-relay-to", "rtmp://localhost:${Dest1Port}/live,rtmp://localhost:${Dest2Port}/live"))) { exit 1 }

# Publish first (10s, background)
$pubLog = Join-Path $script:LogDir "$TestName-pub.log"
$pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error", "-re",
    "-f", "lavfi", "-i", "testsrc=duration=10:size=320x240:rate=25",
    "-f", "lavfi", "-i", "sine=frequency=440:duration=10",
    "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
    "-c:a", "aac", "-b:a", "64k",
    "-f", "flv", "rtmp://localhost:${Port}/live/multi-relay"
) -RedirectStandardOutput $pubLog -RedirectStandardError "$pubLog.err" -NoNewWindow -PassThru

Start-Sleep -Seconds 2

$cap1 = Join-Path $script:TmpDir "relay-cap1.flv"
$cap2 = Join-Path $script:TmpDir "relay-cap2.flv"

Start-Capture -Url "rtmp://localhost:${Dest1Port}/live/multi-relay" -Output $cap1 -Timeout 12
$s1Pid = $script:CapturePid
Start-Capture -Url "rtmp://localhost:${Dest2Port}/live/multi-relay" -Output $cap2 -Timeout 12
$s2Pid = $script:CapturePid

$pubProc.WaitForExit()
Start-Sleep -Seconds 2
Wait-AndStopCapture -Pid $s1Pid
Wait-AndStopCapture -Pid $s2Pid

if (-not $d1.HasExited) { $d1.Kill() }
if (-not $d2.HasExited) { $d2.Kill() }

Assert-FileExists -File $cap1 -Label "Destination 1 capture"
Assert-FileExists -File $cap2 -Label "Destination 2 capture"
Assert-VideoCodec -File $cap1 -Expected "h264"
Assert-VideoCodec -File $cap2 -Expected "h264"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
