# ============================================================================
# TEST: auth-token-publish-allowed
# GROUP: Authentication
#
# WHAT IS TESTED:
#   Publishing with the correct token succeeds (no rejection).
#
# EXPECTED RESULT:
#   - Connection registered, no auth failure messages
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "auth-token-publish-allowed"
$Port = Get-UniquePort $TestName

Setup $TestName
if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-auth-mode", "token", "-auth-token", "live/auth-test=secret123"))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing with correct token (5s)..." -ForegroundColor Blue
Publish-TestPattern -Url "rtmp://localhost:${Port}/live/auth-test?token=secret123" -Duration 5
Start-Sleep -Seconds 2

Assert-LogContains -File $script:ServerLog -Pattern "connection registered" -Label "Publisher connected"
Assert-LogNotContains -File $script:ServerLog -Pattern "auth_failed|authentication failed|rejected" -Label "No auth failures"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
