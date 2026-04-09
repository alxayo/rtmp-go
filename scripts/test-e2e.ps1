# test-e2e.ps1 — End-to-end test suite for go-rtmp (Windows)
# Usage: .\test-e2e.ps1 [-Test TEST_NAME]
#   Run all tests or a specific test by name.
#   Tests: rtmp-basic, rtmps-basic, rtmp-hls, rtmps-hls, auth-allow, auth-reject, rtmps-auth
param(
    [string]$Test = ""
)

$ErrorActionPreference = "Continue"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$ProjectRoot = Split-Path -Parent $ScriptDir
$LogDir = Join-Path $ScriptDir "logs"
$Binary = Join-Path $ProjectRoot "rtmp-server.exe"

# Test counters
$script:Passed = 0
$script:Failed = 0
$script:Skipped = 0
$script:Pids = @()

function Cleanup {
    foreach ($pid in $script:Pids) {
        try {
            $proc = Get-Process -Id $pid -ErrorAction SilentlyContinue
            if ($proc -and -not $proc.HasExited) {
                Stop-Process -Id $pid -Force -ErrorAction SilentlyContinue
            }
        } catch {}
    }
    $script:Pids = @()
    Remove-Item (Join-Path $ScriptDir ".test-tmp") -Recurse -Force -ErrorAction SilentlyContinue
}

# Register cleanup
$null = Register-EngineEvent -SourceIdentifier PowerShell.Exiting -Action { Cleanup }
trap { Cleanup }

function Log($msg) { Write-Host "$(Get-Date -Format 'HH:mm:ss') $msg" }

function Pass-Test($name) {
    Write-Host "  PASS: $name" -ForegroundColor Green
    $script:Passed++
}

function Fail-Test($name, $reason = "") {
    Write-Host "  FAIL: $name" -ForegroundColor Red
    if ($reason) { Write-Host "        $reason" -ForegroundColor Red }
    $script:Failed++
}

function Skip-Test($name, $reason = "") {
    Write-Host "  SKIP: $name" -ForegroundColor Yellow
    if ($reason) { Write-Host "        $reason" -ForegroundColor Yellow }
    $script:Skipped++
}

function Should-Run($testName) {
    return ([string]::IsNullOrEmpty($Test) -or $Test -eq $testName)
}

function Wait-ForLog($file, $pattern, $timeout = 15) {
    for ($i = 0; $i -lt $timeout; $i++) {
        if ((Test-Path $file) -and (Select-String -Path $file -Pattern $pattern -Quiet -ErrorAction SilentlyContinue)) {
            return $true
        }
        Start-Sleep -Seconds 1
    }
    return $false
}

function Start-Server {
    param([int]$Port, [string[]]$ExtraArgs)
    $logFile = Join-Path $LogDir "test-server-${Port}.log"
    $errFile = Join-Path $LogDir "test-server-${Port}-err.log"
    $allArgs = @("-listen", ":$Port") + $ExtraArgs
    $proc = Start-Process -FilePath $Binary -ArgumentList $allArgs `
        -RedirectStandardOutput $logFile -RedirectStandardError $errFile `
        -NoNewWindow -PassThru
    $script:Pids += $proc.Id
    if (-not (Wait-ForLog $logFile "server started|server listening" 10)) {
        Log "    Server failed to start on port $Port"
        if (Test-Path $logFile) { Get-Content $logFile | Write-Host }
        if (Test-Path $errFile) { Get-Content $errFile | Write-Host }
        return $null
    }
    return $proc.Id
}

function Stop-Proc($pid) {
    try {
        $p = Get-Process -Id $pid -ErrorAction SilentlyContinue
        if ($p -and -not $p.HasExited) {
            Stop-Process -Id $pid -Force -ErrorAction SilentlyContinue
            Start-Sleep -Milliseconds 500
        }
    } catch {}
    $script:Pids = $script:Pids | Where-Object { $_ -ne $pid }
}

# =========================
# Pre-flight
# =========================
Write-Host "=== go-rtmp End-to-End Test Suite ===" -ForegroundColor Cyan
Write-Host ""

& "$ScriptDir\check-deps.ps1"
if ($LASTEXITCODE -ne 0) { Write-Host "Dependencies missing. Aborting."; exit 1 }
Write-Host ""

Write-Host "Building server..." -ForegroundColor Cyan
Push-Location $ProjectRoot
& go build -o $Binary ./cmd/rtmp-server
if ($LASTEXITCODE -ne 0) { Pop-Location; throw "Build failed" }
Pop-Location
Write-Host "Built: $Binary"
Write-Host ""

New-Item -ItemType Directory -Path $LogDir -Force | Out-Null
$TmpDir = Join-Path $ScriptDir ".test-tmp"
New-Item -ItemType Directory -Path $TmpDir -Force | Out-Null

# =========================
# Test 1: RTMP Publish + Capture
# =========================
if (Should-Run "rtmp-basic") {
    Log "Test 1: RTMP Publish + Capture"
    $port = 19351
    $serverPid = Start-Server -Port $port -ExtraArgs @("-log-level", "debug")

    if ($serverPid) {
        $captureFile = Join-Path $TmpDir "rtmp-basic-capture.flv"

        # Start subscriber/capture
        $capProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
            "-hide_banner", "-loglevel", "error",
            "-i", "rtmp://localhost:${port}/live/test",
            "-t", "8", "-c", "copy", $captureFile
        ) -NoNewWindow -PassThru -RedirectStandardError (Join-Path $LogDir "test-capture-rtmp.log")
        $script:Pids += $capProc.Id

        Start-Sleep -Seconds 1

        # Publish test pattern
        $pubLog = Join-Path $LogDir "test-publish-rtmp.log"
        $pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
            "-hide_banner", "-loglevel", "error",
            "-f", "lavfi", "-i", "testsrc=duration=5:size=320x240:rate=25",
            "-f", "lavfi", "-i", "sine=frequency=440:duration=5",
            "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
            "-c:a", "aac", "-b:a", "64k",
            "-f", "flv", "rtmp://localhost:${port}/live/test"
        ) -NoNewWindow -PassThru -Wait -RedirectStandardError $pubLog

        Start-Sleep -Seconds 3
        Stop-Proc $capProc.Id

        if ((Test-Path $captureFile) -and (Get-Item $captureFile).Length -gt 0) {
            try {
                $probeOut = & ffprobe -v error -show_entries format=duration -of csv=p=0 $captureFile 2>&1
                $hasVideo = & ffprobe -v error -select_streams v -show_entries stream=codec_type -of csv=p=0 $captureFile 2>&1
                if ($hasVideo -and [double]$probeOut -gt 2.0) {
                    Pass-Test "rtmp-basic (duration=${probeOut}s, has video)"
                } else {
                    Fail-Test "rtmp-basic" "capture invalid: duration=$probeOut, video=$hasVideo"
                }
            } catch {
                Fail-Test "rtmp-basic" "ffprobe failed: $_"
            }
        } else {
            Fail-Test "rtmp-basic" "no capture file or empty"
        }

        Stop-Proc $serverPid
    }
}

# =========================
# Test 2: RTMPS Publish + Capture
# =========================
if (Should-Run "rtmps-basic") {
    Log "Test 2: RTMPS Publish + Capture (dual listener)"
    $port = 19352
    $tlsPort = 19362

    & "$ScriptDir\generate-certs.ps1" 2>$null
    $cert = Join-Path $ScriptDir ".certs\cert.pem"
    $key = Join-Path $ScriptDir ".certs\key.pem"

    $serverPid = Start-Server -Port $port -ExtraArgs @(
        "-log-level", "debug",
        "-tls-listen", ":$tlsPort", "-tls-cert", $cert, "-tls-key", $key
    )

    if ($serverPid) {
        # Publish to plain RTMP
        $pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
            "-hide_banner", "-loglevel", "error",
            "-f", "lavfi", "-i", "testsrc=duration=3:size=320x240:rate=25",
            "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
            "-f", "flv", "rtmp://localhost:${port}/live/tls_test"
        ) -NoNewWindow -PassThru -Wait -RedirectStandardError (Join-Path $LogDir "test-publish-rtmps.log")

        Start-Sleep -Seconds 2

        $serverLog = Join-Path $LogDir "test-server-${port}.log"
        $tlsOk = Select-String -Path $serverLog -Pattern "RTMPS server listening" -Quiet -ErrorAction SilentlyContinue
        $connOk = Select-String -Path $serverLog -Pattern "connection registered" -Quiet -ErrorAction SilentlyContinue

        if ($tlsOk -and $connOk) {
            Pass-Test "rtmps-basic (dual listener active, RTMP publish verified)"
        } elseif (-not $tlsOk) {
            Fail-Test "rtmps-basic" "RTMPS listener not started"
        } else {
            Fail-Test "rtmps-basic" "no connection registered"
        }

        Stop-Proc $serverPid
    }
}

# =========================
# Test 3: RTMP + HLS via Hook
# =========================
if (Should-Run "rtmp-hls") {
    Log "Test 3: RTMP + HLS via Hook"
    $port = 19353
    $hlsOut = Join-Path $ProjectRoot "hls-output\live_hls_test"

    Remove-Item $hlsOut -Recurse -Force -ErrorAction SilentlyContinue

    $hookScript = Join-Path $ScriptDir "on-publish-hls.ps1"
    $serverPid = Start-Server -Port $port -ExtraArgs @(
        "-log-level", "debug",
        "-hook-script", "publish_start=$hookScript"
    )

    if ($serverPid) {
        $env:RTMP_PORT = "$port"

        # Publish 10-second stream
        $pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
            "-hide_banner", "-loglevel", "error",
            "-f", "lavfi", "-i", "testsrc=duration=10:size=320x240:rate=25",
            "-f", "lavfi", "-i", "sine=frequency=440:duration=10",
            "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
            "-c:a", "aac", "-b:a", "64k",
            "-f", "flv", "rtmp://localhost:${port}/live/hls_test"
        ) -NoNewWindow -PassThru -Wait -RedirectStandardError (Join-Path $LogDir "test-publish-hls.log")

        Start-Sleep -Seconds 3

        $playlist = Join-Path $hlsOut "playlist.m3u8"
        if ((Test-Path $playlist) -and (Select-String -Path $playlist -Pattern "#EXTM3U" -Quiet -ErrorAction SilentlyContinue)) {
            $tsCount = (Get-ChildItem $hlsOut -Filter "*.ts" -ErrorAction SilentlyContinue | Measure-Object).Count
            if ($tsCount -gt 0) {
                Pass-Test "rtmp-hls (playlist valid, $tsCount segment(s))"
            } else {
                Fail-Test "rtmp-hls" "playlist exists but no .ts segments"
            }
        } else {
            Fail-Test "rtmp-hls" "no playlist.m3u8 created (hook may not have fired)"
        }

        # Kill hook ffmpeg
        $pidFile = Join-Path $hlsOut ".ffmpeg.pid"
        if (Test-Path $pidFile) {
            $hookPid = Get-Content $pidFile -Raw
            Stop-Process -Id ([int]$hookPid.Trim()) -Force -ErrorAction SilentlyContinue
        }

        Stop-Proc $serverPid
    }
}

# =========================
# Test 4: RTMPS + HLS via Hook
# =========================
if (Should-Run "rtmps-hls") {
    Log "Test 4: RTMPS + HLS via Hook"
    $port = 19354
    $tlsPort = 19364
    $hlsOut = Join-Path $ProjectRoot "hls-output\live_rtmps_hls_test"

    & "$ScriptDir\generate-certs.ps1" 2>$null
    $cert = Join-Path $ScriptDir ".certs\cert.pem"
    $key = Join-Path $ScriptDir ".certs\key.pem"

    Remove-Item $hlsOut -Recurse -Force -ErrorAction SilentlyContinue

    $hookScript = Join-Path $ScriptDir "on-publish-hls.ps1"
    $serverPid = Start-Server -Port $port -ExtraArgs @(
        "-log-level", "debug",
        "-tls-listen", ":$tlsPort", "-tls-cert", $cert, "-tls-key", $key,
        "-hook-script", "publish_start=$hookScript"
    )

    if ($serverPid) {
        $env:RTMP_PORT = "$port"

        $pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
            "-hide_banner", "-loglevel", "error",
            "-f", "lavfi", "-i", "testsrc=duration=10:size=320x240:rate=25",
            "-f", "lavfi", "-i", "sine=frequency=440:duration=10",
            "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
            "-c:a", "aac", "-b:a", "64k",
            "-f", "flv", "rtmp://localhost:${port}/live/rtmps_hls_test"
        ) -NoNewWindow -PassThru -Wait -RedirectStandardError (Join-Path $LogDir "test-publish-rtmps-hls.log")

        Start-Sleep -Seconds 3

        $serverLog = Join-Path $LogDir "test-server-${port}.log"
        $playlist = Join-Path $hlsOut "playlist.m3u8"

        $hlsOk = (Test-Path $playlist) -and (Select-String -Path $playlist -Pattern "#EXTM3U" -Quiet -ErrorAction SilentlyContinue)
        $tlsOk = Select-String -Path $serverLog -Pattern "RTMPS server listening" -Quiet -ErrorAction SilentlyContinue

        if ($hlsOk -and $tlsOk) {
            Pass-Test "rtmps-hls (HLS + TLS listener active)"
        } elseif ($tlsOk) {
            Fail-Test "rtmps-hls" "TLS active but HLS not generated"
        } else {
            Fail-Test "rtmps-hls" "TLS listener not started"
        }

        $pidFile = Join-Path $hlsOut ".ffmpeg.pid"
        if (Test-Path $pidFile) {
            $hookPid = Get-Content $pidFile -Raw
            Stop-Process -Id ([int]$hookPid.Trim()) -Force -ErrorAction SilentlyContinue
        }

        Stop-Proc $serverPid
    }
}

# =========================
# Test 5: RTMP + Auth (allowed)
# =========================
if (Should-Run "auth-allow") {
    Log "Test 5: RTMP + Auth (allowed)"
    $port = 19355

    $serverPid = Start-Server -Port $port -ExtraArgs @(
        "-log-level", "debug",
        "-auth-mode", "token", "-auth-token", "live/test=secret123"
    )

    if ($serverPid) {
        $captureFile = Join-Path $TmpDir "auth-allow-capture.flv"

        # Capture with valid token
        $capProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
            "-hide_banner", "-loglevel", "error",
            "-i", "rtmp://localhost:${port}/live/test?token=secret123",
            "-t", "8", "-c", "copy", $captureFile
        ) -NoNewWindow -PassThru -RedirectStandardError (Join-Path $LogDir "test-capture-auth.log")
        $script:Pids += $capProc.Id

        Start-Sleep -Seconds 1

        # Publish with valid token
        $pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
            "-hide_banner", "-loglevel", "error",
            "-f", "lavfi", "-i", "testsrc=duration=5:size=320x240:rate=25",
            "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
            "-f", "flv", "rtmp://localhost:${port}/live/test?token=secret123"
        ) -NoNewWindow -PassThru -Wait -RedirectStandardError (Join-Path $LogDir "test-publish-auth.log")

        Start-Sleep -Seconds 3
        Stop-Proc $capProc.Id

        $serverLog = Join-Path $LogDir "test-server-${port}.log"
        $connOk = Select-String -Path $serverLog -Pattern "connection registered" -Quiet -ErrorAction SilentlyContinue
        $authFail = Select-String -Path $serverLog -Pattern "auth_failed|authentication failed" -Quiet -ErrorAction SilentlyContinue

        if ($connOk -and -not $authFail) {
            Pass-Test "auth-allow (publish with valid token succeeded)"
        } elseif ($authFail) {
            Fail-Test "auth-allow" "auth failed despite valid token"
        } else {
            Fail-Test "auth-allow" "no connection in server log"
        }

        Stop-Proc $serverPid
    }
}

# =========================
# Test 6: RTMP + Auth (rejected)
# =========================
if (Should-Run "auth-reject") {
    Log "Test 6: RTMP + Auth (rejected)"
    $port = 19356

    $serverPid = Start-Server -Port $port -ExtraArgs @(
        "-log-level", "debug",
        "-auth-mode", "token", "-auth-token", "live/test=secret123"
    )

    if ($serverPid) {
        # Publish with WRONG token
        $pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
            "-hide_banner", "-loglevel", "error",
            "-f", "lavfi", "-i", "testsrc=duration=3:size=320x240:rate=25",
            "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
            "-f", "flv", "rtmp://localhost:${port}/live/test?token=wrongtoken"
        ) -NoNewWindow -PassThru -Wait -RedirectStandardError (Join-Path $LogDir "test-publish-auth-reject.log")

        Start-Sleep -Seconds 2

        $serverLog = Join-Path $LogDir "test-server-${port}.log"
        $authFail = Select-String -Path $serverLog -Pattern "auth_failed|authentication failed|ErrUnauthorized" -Quiet -ErrorAction SilentlyContinue
        $publishOk = Select-String -Path $serverLog -Pattern "publish started" -Quiet -ErrorAction SilentlyContinue

        if ($authFail) {
            Pass-Test "auth-reject (invalid token correctly rejected)"
        } elseif (-not $publishOk) {
            Pass-Test "auth-reject (publish blocked — no publish_start in log)"
        } else {
            Fail-Test "auth-reject" "publish succeeded with wrong token"
        }

        Stop-Proc $serverPid
    }
}

# =========================
# Test 7: RTMPS + Auth
# =========================
if (Should-Run "rtmps-auth") {
    Log "Test 7: RTMPS + Auth (TLS + token)"
    $port = 19357
    $tlsPort = 19367

    & "$ScriptDir\generate-certs.ps1" 2>$null
    $cert = Join-Path $ScriptDir ".certs\cert.pem"
    $key = Join-Path $ScriptDir ".certs\key.pem"

    $serverPid = Start-Server -Port $port -ExtraArgs @(
        "-log-level", "debug",
        "-tls-listen", ":$tlsPort", "-tls-cert", $cert, "-tls-key", $key,
        "-auth-mode", "token", "-auth-token", "live/test=secret123"
    )

    if ($serverPid) {
        # Publish with valid token
        $pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
            "-hide_banner", "-loglevel", "error",
            "-f", "lavfi", "-i", "testsrc=duration=3:size=320x240:rate=25",
            "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
            "-f", "flv", "rtmp://localhost:${port}/live/test?token=secret123"
        ) -NoNewWindow -PassThru -Wait -RedirectStandardError (Join-Path $LogDir "test-publish-rtmps-auth.log")

        Start-Sleep -Seconds 2

        $serverLog = Join-Path $LogDir "test-server-${port}.log"
        $tlsOk = Select-String -Path $serverLog -Pattern "RTMPS server listening" -Quiet -ErrorAction SilentlyContinue
        $connOk = Select-String -Path $serverLog -Pattern "connection registered" -Quiet -ErrorAction SilentlyContinue
        $authFail = Select-String -Path $serverLog -Pattern "auth_failed" -Quiet -ErrorAction SilentlyContinue

        if ($tlsOk -and $connOk -and -not $authFail) {
            Pass-Test "rtmps-auth (TLS + auth both active, publish succeeded)"
        } elseif (-not $tlsOk) {
            Fail-Test "rtmps-auth" "TLS listener not started"
        } else {
            Fail-Test "rtmps-auth" "auth failed with valid token"
        }

        Stop-Proc $serverPid
    }
}

# =========================
# Summary
# =========================
Write-Host ""
Write-Host "=== Test Summary ===" -ForegroundColor Cyan
$total = $script:Passed + $script:Failed + $script:Skipped
Write-Host "  Total:   $total"
Write-Host "  Passed:  $($script:Passed)" -ForegroundColor Green
Write-Host "  Failed:  $($script:Failed)" -ForegroundColor Red
Write-Host "  Skipped: $($script:Skipped)" -ForegroundColor Yellow
Write-Host ""

if ($script:Failed -gt 0) {
    Write-Host "Some tests failed. Check logs in $LogDir" -ForegroundColor Red
    exit $script:Failed
} else {
    Write-Host "All tests passed!" -ForegroundColor Green
    exit 0
}
