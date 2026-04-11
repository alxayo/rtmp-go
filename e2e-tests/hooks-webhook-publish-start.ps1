# ============================================================================
# TEST: hooks-webhook-publish-start
# GROUP: Event Hooks
#
# WHAT IS TESTED:
#   Webhook POST fires on publish_start event. Uses a Python HTTP listener
#   to capture the POST body.
#
# EXPECTED RESULT:
#   - Webhook endpoint receives POST with event JSON
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "hooks-webhook-publish-start"
$Port = Get-UniquePort $TestName
$WebhookPort = $Port + 300

if (-not (Get-Command python3 -ErrorAction SilentlyContinue)) {
    if (-not (Get-Command python -ErrorAction SilentlyContinue)) {
        Write-Host "SKIP: python3/python not found" -ForegroundColor Yellow
        exit 2
    }
    $python = "python"
} else { $python = "python3" }

Setup $TestName

$webhookLog = Join-Path $script:TmpDir "webhook-received.json"
$webhookServer = Join-Path $script:TmpDir "webhook-server.py"

@"
import http.server, json, sys, os
class Handler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get('Content-Length', 0))
        body = self.rfile.read(length)
        with open(r"$webhookLog", 'wb') as f:
            f.write(body)
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b'ok')
    def log_message(self, format, *args):
        pass
server = http.server.HTTPServer(('localhost', $WebhookPort), Handler)
server.timeout = 30
server.handle_request()
"@ | Set-Content -Path $webhookServer

$pyProc = Start-Process -FilePath $python -ArgumentList $webhookServer -NoNewWindow -PassThru
Start-Sleep -Seconds 1

if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-hook-webhook", "http://localhost:${WebhookPort}/hook"))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing to trigger webhook (3s)..." -ForegroundColor Blue
Publish-TestPattern -Url "rtmp://localhost:${Port}/live/webhook-test" -Duration 3
Start-Sleep -Seconds 3

if (-not $pyProc.HasExited) { $pyProc.WaitForExit(5000) }

if ((Test-Path $webhookLog) -and (Get-Item $webhookLog).Length -gt 0) {
    Pass-Check "Webhook received POST data ($((Get-Item $webhookLog).Length) bytes)"
} else {
    Fail-Check "Webhook received POST" "No data received"
}

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
