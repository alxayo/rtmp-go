# ============================================================================
# TEST: rtmps-dual-listener
# GROUP: RTMPS (TLS)
#
# WHAT IS TESTED:
#   Both plain RTMP and RTMPS (TLS) listeners active simultaneously.
#
# EXPECTED RESULT:
#   - Both listeners start, plain RTMP publish succeeds alongside TLS
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "rtmps-dual-listener"
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

Assert-LogContains -File $script:ServerLog -Pattern "listening" -Label "Server shows listener(s) started"

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing over plain RTMP (3s)..." -ForegroundColor Blue
Publish-TestPattern -Url "rtmp://localhost:${Port}/live/dual-test" -Duration 3
Start-Sleep -Seconds 2

Assert-LogContains -File $script:ServerLog -Pattern "connection registered" -Label "Plain RTMP connection accepted"
Assert-LogNotContains -File $script:ServerLog -Pattern "panic|FATAL" -Label "No server panics"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
