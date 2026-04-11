# ============================================================================
# TEST: hooks-shell-publish-start
# GROUP: Event Hooks
#
# WHAT IS TESTED:
#   Shell hook fires on publish_start event and creates a marker file.
#
# EXPECTED RESULT:
#   - Marker file created by the hook script after publish
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "hooks-shell-publish-start"
$Port = Get-UniquePort $TestName

Setup $TestName

$markerFile = Join-Path $script:TmpDir "hook-fired.txt"
$hookScript = Join-Path $script:TmpDir "hook.ps1"

# Create hook script that writes marker
@"
`$input | Out-File -FilePath "$markerFile" -Encoding UTF8
"@ | Set-Content -Path $hookScript

if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-hook-script", "powershell.exe -File $hookScript"))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing to trigger hook (3s)..." -ForegroundColor Blue
Publish-TestPattern -Url "rtmp://localhost:${Port}/live/hook-test" -Duration 3
Start-Sleep -Seconds 3

if (Test-Path $markerFile) {
    Pass-Check "Hook script fired (marker file created)"
    if ((Get-Item $markerFile).Length -gt 0) {
        Pass-Check "Hook received event data ($((Get-Item $markerFile).Length) bytes)"
    }
} else {
    Fail-Check "Hook script fired" "Marker file not found"
}

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
