# ============================================================================
# TEST: rtmps-publish-play
# GROUP: RTMPS (TLS)
#
# WHAT IS TESTED:
#   Publish and subscribe over RTMPS (TLS). Uses self-signed cert.
#
# EXPECTED RESULT:
#   - TLS publish/subscribe works, captured file has H.264 video
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "rtmps-publish-play"
$Port = Get-UniquePort $TestName
$TlsPort = $Port + 100

Setup $TestName
Generate-Certs

if (-not (Start-TestServer -Port $Port -ExtraArgs @(
    "-log-level", "debug",
    "-tls-listen", "localhost:${TlsPort}",
    "-tls-cert", (Join-Path $script:CertsDir "server.crt"),
    "-tls-key", (Join-Path $script:CertsDir "server.key")
))) { exit 1 }

# Publisher first (8s, background)
$pubLog = Join-Path $script:LogDir "$TestName-publish.log"
Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing over RTMPS (8s, background)..." -ForegroundColor Blue
$pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error", "-re",
    "-f", "lavfi", "-i", "testsrc=duration=8:size=320x240:rate=25",
    "-f", "lavfi", "-i", "sine=frequency=440:duration=8",
    "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
    "-c:a", "aac", "-b:a", "64k", "-tls_verify", "0",
    "-f", "flv", "rtmps://localhost:${TlsPort}/live/tls-test"
) -RedirectStandardOutput $pubLog -RedirectStandardError "$pubLog.err" -NoNewWindow -PassThru

Start-Sleep -Seconds 2
$capture = Join-Path $script:TmpDir "capture.flv"
Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Starting RTMPS subscriber..." -ForegroundColor Blue
$capLog = Join-Path $script:LogDir "$TestName-capture.log"
$capProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error", "-tls_verify", "0",
    "-i", "rtmps://localhost:${TlsPort}/live/tls-test",
    "-c", "copy", "-t", "10", $capture
) -RedirectStandardOutput $capLog -RedirectStandardError "$capLog.err" -NoNewWindow -PassThru

$pubProc.WaitForExit()
Start-Sleep -Seconds 2
if (-not $capProc.HasExited) { $capProc.Kill() }

Assert-FileExists -File $capture -Label "RTMPS capture file exists"
Assert-VideoCodec -File $capture -Expected "h264"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
