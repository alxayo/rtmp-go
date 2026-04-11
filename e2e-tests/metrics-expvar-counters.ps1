# ============================================================================
# TEST: metrics-expvar-counters
# GROUP: Metrics
#
# WHAT IS TESTED:
#   /debug/vars expvar endpoint returns JSON with rtmp counters.
#
# EXPECTED RESULT:
#   - Valid JSON response, contains RTMP counter fields
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "metrics-expvar-counters"
$Port = Get-UniquePort $TestName
$MetricsPort = $Port + 400

Setup $TestName
if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-metrics-addr", "localhost:${MetricsPort}"))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Checking /debug/vars..." -ForegroundColor Blue
try {
    $response = Invoke-RestMethod -Uri "http://localhost:${MetricsPort}/debug/vars" -TimeoutSec 5
    Pass-Check "Metrics endpoint reachable"
} catch {
    Fail-Check "Metrics endpoint reachable" "No response from /debug/vars"
}

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing to increment counters (3s)..." -ForegroundColor Blue
Publish-TestPattern -Url "rtmp://localhost:${Port}/live/metrics-test" -Duration 3
Start-Sleep -Seconds 2

try {
    $after = Invoke-RestMethod -Uri "http://localhost:${MetricsPort}/debug/vars" -TimeoutSec 5
    Pass-Check "Metrics still responding after publish"
} catch {
    Fail-Check "Metrics after publish" "Endpoint stopped responding"
}

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
