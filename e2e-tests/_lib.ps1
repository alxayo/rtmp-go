# ============================================================================
# _lib.ps1 — Shared helper library for go-rtmp E2E tests (Windows/PowerShell)
#
# This file is dot-sourced by every test script. It provides:
#   - Server build/start/stop management
#   - FFmpeg publish/capture helpers
#   - Assertion functions (file exists, codec check, duration, decodability)
#   - Unique port allocation per test
#   - Cleanup and temp directory management
#   - Test result reporting
#
# USAGE (in a test script):
#   . "$PSScriptRoot\_lib.ps1"
#   Setup "test-name"
#   Start-TestServer -Port $Port [extra flags...]
#   ... test logic ...
#   Teardown
#   Report-Result "test-name"
# ============================================================================

$ErrorActionPreference = "Continue"

# ---- Paths ----
$script:E2EDir = $PSScriptRoot
$script:ProjectRoot = Split-Path -Parent $script:E2EDir
$script:LogDir = Join-Path $script:E2EDir "logs"
$script:CertsDir = Join-Path $script:E2EDir ".certs"
$script:Binary = Join-Path $script:ProjectRoot "rtmp-server.exe"

# ---- State ----
$script:Pids = @()
$script:ChecksPassed = 0
$script:ChecksFailed = 0
$script:TestName = ""
$script:TmpDir = ""
$script:ServerPid = $null
$script:ServerLog = ""
$script:CapturePid = $null

# ---- Port Allocation ----
function Get-UniquePort {
    param([string]$Name)
    $hash = 0
    foreach ($c in $Name.ToCharArray()) { $hash = $hash * 31 + [int]$c }
    return 19400 + ([Math]::Abs($hash) % 200)
}

# ---- Setup / Teardown ----

function Setup {
    param([string]$Name)
    $script:TestName = $Name
    $script:ChecksPassed = 0
    $script:ChecksFailed = 0
    $script:Pids = @()

    New-Item -ItemType Directory -Force -Path $script:LogDir | Out-Null
    $script:TmpDir = Join-Path $script:E2EDir ".test-tmp\$Name"
    if (Test-Path $script:TmpDir) { Remove-Item $script:TmpDir -Recurse -Force }
    New-Item -ItemType Directory -Force -Path $script:TmpDir | Out-Null

    Write-Host ""
    Write-Host "=== E2E Test: $Name ===" -ForegroundColor Blue
}

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
}

function Teardown {
    Cleanup
    if (-not $env:KEEP_TMP) {
        Remove-Item $script:TmpDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# Register cleanup on exit
$null = Register-EngineEvent -SourceIdentifier PowerShell.Exiting -Action { Cleanup } -ErrorAction SilentlyContinue

# ---- Server Management ----

function Build-Server {
    $srcTime = (Get-ChildItem -Recurse "$script:ProjectRoot\cmd", "$script:ProjectRoot\internal" -Filter "*.go" -ErrorAction SilentlyContinue |
        Sort-Object LastWriteTime -Descending | Select-Object -First 1).LastWriteTime
    $binExists = Test-Path $script:Binary
    $binTime = if ($binExists) { (Get-Item $script:Binary).LastWriteTime } else { [DateTime]::MinValue }

    if (-not $binExists -or $srcTime -gt $binTime) {
        Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Building server..." -ForegroundColor Blue
        Push-Location $script:ProjectRoot
        go build -o $script:Binary ./cmd/rtmp-server
        Pop-Location
        Write-Host "$(Get-Date -Format 'HH:mm:ss') OK Server built" -ForegroundColor Green
    }
}

function Wait-ForLog {
    param([string]$File, [string]$Pattern, [int]$Timeout = 15)
    for ($i = 0; $i -lt $Timeout; $i++) {
        if ((Test-Path $File) -and (Select-String -Path $File -Pattern $Pattern -Quiet -ErrorAction SilentlyContinue)) {
            return $true
        }
        Start-Sleep -Seconds 1
    }
    return $false
}

function Start-TestServer {
    param([int]$Port, [string[]]$ExtraArgs = @())
    Build-Server

    $script:ServerLog = Join-Path $script:LogDir "$($script:TestName)-server.log"
    $errLog = Join-Path $script:LogDir "$($script:TestName)-server-err.log"
    $allArgs = @("-listen", ":$Port") + $ExtraArgs
    $proc = Start-Process -FilePath $script:Binary -ArgumentList $allArgs `
        -RedirectStandardOutput $script:ServerLog -RedirectStandardError $errLog `
        -NoNewWindow -PassThru
    $script:ServerPid = $proc.Id
    $script:Pids += $proc.Id

    if (-not (Wait-ForLog $script:ServerLog "server started|server listening|RTMP server listening" 10)) {
        Write-Host "$(Get-Date -Format 'HH:mm:ss') X Server failed to start on port $Port" -ForegroundColor Red
        if (Test-Path $script:ServerLog) { Get-Content $script:ServerLog -Tail 20 }
        return $false
    }
    Write-Host "$(Get-Date -Format 'HH:mm:ss') OK Server started (PID $($script:ServerPid), port $Port)" -ForegroundColor Green
    return $true
}

function Stop-TestServer {
    param([int]$Pid = $script:ServerPid)
    if ($Pid) {
        try {
            $proc = Get-Process -Id $Pid -ErrorAction SilentlyContinue
            if ($proc -and -not $proc.HasExited) {
                Stop-Process -Id $Pid -Force -ErrorAction SilentlyContinue
                Start-Sleep -Seconds 1
            }
        } catch {}
        $script:Pids = $script:Pids | Where-Object { $_ -ne $Pid }
    }
}

# ---- FFmpeg Helpers ----

function Publish-TestPattern {
    param([string]$Url, [int]$Duration, [string[]]$ExtraArgs = @())
    $logFile = Join-Path $script:LogDir "$($script:TestName)-publish.log"
    $args = @("-hide_banner", "-loglevel", "error", "-re",
        "-f", "lavfi", "-i", "testsrc=duration=${Duration}:size=320x240:rate=25",
        "-f", "lavfi", "-i", "sine=frequency=440:duration=$Duration",
        "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
        "-c:a", "aac", "-b:a", "64k") + $ExtraArgs + @("-f", "flv", $Url)
    $proc = Start-Process -FilePath "ffmpeg" -ArgumentList $args `
        -RedirectStandardOutput $logFile -RedirectStandardError "$logFile.err" `
        -NoNewWindow -PassThru -Wait
}

function Publish-H265TestPattern {
    param([string]$Url, [int]$Duration, [string[]]$ExtraArgs = @())
    $logFile = Join-Path $script:LogDir "$($script:TestName)-publish-h265.log"
    $args = @("-hide_banner", "-loglevel", "warning", "-re",
        "-f", "lavfi", "-i", "testsrc2=duration=${Duration}:size=640x480:rate=30",
        "-f", "lavfi", "-i", "sine=frequency=440:duration=$Duration",
        "-c:v", "libx265", "-preset", "ultrafast",
        "-c:a", "aac", "-b:a", "64k") + $ExtraArgs + @("-f", "flv", $Url)
    $proc = Start-Process -FilePath "ffmpeg" -ArgumentList $args `
        -RedirectStandardOutput $logFile -RedirectStandardError "$logFile.err" `
        -NoNewWindow -PassThru -Wait
}

function Publish-SrtH264 {
    param([string]$Url, [int]$Duration, [string[]]$ExtraArgs = @())
    $logFile = Join-Path $script:LogDir "$($script:TestName)-publish-srt.log"
    $args = @("-hide_banner", "-loglevel", "error", "-re",
        "-f", "lavfi", "-i", "testsrc=duration=${Duration}:size=320x240:rate=25",
        "-f", "lavfi", "-i", "sine=frequency=440:duration=$Duration",
        "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
        "-c:a", "aac", "-b:a", "64k") + $ExtraArgs + @("-f", "mpegts", $Url)
    $proc = Start-Process -FilePath "ffmpeg" -ArgumentList $args `
        -RedirectStandardOutput $logFile -RedirectStandardError "$logFile.err" `
        -NoNewWindow -PassThru -Wait
}

function Publish-SrtH265 {
    param([string]$Url, [int]$Duration, [string[]]$ExtraArgs = @())
    $logFile = Join-Path $script:LogDir "$($script:TestName)-publish-srt-h265.log"
    $args = @("-hide_banner", "-loglevel", "error", "-re",
        "-f", "lavfi", "-i", "testsrc2=duration=${Duration}:size=640x480:rate=30",
        "-f", "lavfi", "-i", "sine=frequency=440:duration=$Duration",
        "-c:v", "libx265", "-preset", "ultrafast",
        "-c:a", "aac", "-b:a", "64k") + $ExtraArgs + @("-f", "mpegts", $Url)
    $proc = Start-Process -FilePath "ffmpeg" -ArgumentList $args `
        -RedirectStandardOutput $logFile -RedirectStandardError "$logFile.err" `
        -NoNewWindow -PassThru -Wait
}

function Start-Capture {
    param([string]$Url, [string]$Output, [int]$Timeout)
    $logFile = Join-Path $script:LogDir "$($script:TestName)-capture.log"
    # -rw_timeout: microsecond I/O timeout; prevents ffmpeg from hanging
    # indefinitely when the publisher disconnects mid-stream.
    $rwTimeout = ($Timeout + 5) * 1000000
    $args = @("-hide_banner", "-loglevel", "error",
        "-rw_timeout", $rwTimeout,
        "-i", $Url, "-t", $Timeout, "-c", "copy", $Output)
    $proc = Start-Process -FilePath "ffmpeg" -ArgumentList $args `
        -RedirectStandardOutput $logFile -RedirectStandardError "$logFile.err" `
        -NoNewWindow -PassThru
    $script:CapturePid = $proc.Id
    $script:Pids += $proc.Id
    return $proc
}

function Wait-AndStopCapture {
    param([int]$Pid = $script:CapturePid, [int]$Timeout = 15)
    for ($i = 0; $i -lt $Timeout; $i++) {
        try {
            $proc = Get-Process -Id $Pid -ErrorAction SilentlyContinue
            if (-not $proc -or $proc.HasExited) { break }
        } catch { break }
        Start-Sleep -Seconds 1
    }
    try {
        Stop-Process -Id $Pid -Force -ErrorAction SilentlyContinue
    } catch {}
    $script:Pids = $script:Pids | Where-Object { $_ -ne $Pid }
}

# ---- TLS Certificates ----

function New-TestCerts {
    if ((Test-Path "$script:CertsDir\cert.pem") -and (Test-Path "$script:CertsDir\key.pem")) {
        return
    }
    New-Item -ItemType Directory -Force -Path $script:CertsDir | Out-Null
    Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Generating TLS certificates..." -ForegroundColor Blue
    openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 `
        -nodes -keyout "$script:CertsDir\key.pem" -out "$script:CertsDir\cert.pem" `
        -days 365 -subj "/CN=localhost" `
        -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" 2>$null
    Write-Host "$(Get-Date -Format 'HH:mm:ss') OK Certificates generated" -ForegroundColor Green
}

# ---- Assertions ----

function Pass-Check {
    param([string]$Message)
    Write-Host "  OK $Message" -ForegroundColor Green
    $script:ChecksPassed++
}

function Fail-Check {
    param([string]$Message, [string]$Detail = "")
    Write-Host "  X $Message" -ForegroundColor Red
    if ($Detail) { Write-Host "    $Detail" -ForegroundColor Red }
    $script:ChecksFailed++
}

function Assert-FileExists {
    param([string]$File, [string]$Label = "File exists")
    if ((Test-Path $File) -and (Get-Item $File).Length -gt 0) {
        $size = (Get-Item $File).Length
        Pass-Check "$Label ($(Split-Path -Leaf $File), $size bytes)"
    } else {
        Fail-Check $Label "File not found or empty: $File"
    }
}

function Assert-VideoCodec {
    param([string]$File, [string]$Expected)
    $codec = & ffprobe -v error -select_streams v:0 -show_entries stream=codec_name -of csv=p=0 $File 2>$null
    $codec = $codec.Trim()
    if ($codec -eq $Expected) {
        Pass-Check "Video codec is $Expected"
    } else {
        Fail-Check "Video codec is $Expected" "got: '$codec'"
    }
}

function Assert-AudioCodec {
    param([string]$File, [string]$Expected)
    $codec = & ffprobe -v error -select_streams a:0 -show_entries stream=codec_name -of csv=p=0 $File 2>$null
    $codec = $codec.Trim()
    if ($codec -eq $Expected) {
        Pass-Check "Audio codec is $Expected"
    } else {
        Fail-Check "Audio codec is $Expected" "got: '$codec'"
    }
}

function Assert-HasVideo {
    param([string]$File)
    $type = & ffprobe -v error -select_streams v:0 -show_entries stream=codec_type -of csv=p=0 $File 2>$null
    if ($type.Trim() -eq "video") {
        Pass-Check "File has video stream"
    } else {
        Fail-Check "File has video stream" "no video stream found"
    }
}

function Assert-HasAudio {
    param([string]$File)
    $type = & ffprobe -v error -select_streams a:0 -show_entries stream=codec_type -of csv=p=0 $File 2>$null
    if ($type.Trim() -eq "audio") {
        Pass-Check "File has audio stream"
    } else {
        Fail-Check "File has audio stream" "no audio stream found"
    }
}

function Assert-Duration {
    param([string]$File, [double]$Min, [double]$Max)
    $dur = & ffprobe -v error -show_entries format=duration -of csv=p=0 $File 2>$null
    $d = [double]$dur
    if ($d -ge $Min -and $d -le $Max) {
        Pass-Check "Duration in range [${Min}s, ${Max}s] (got ${d}s)"
    } else {
        Fail-Check "Duration in range [${Min}s, ${Max}s]" "got ${d}s"
    }
}

function Assert-Decodable {
    param([string]$File)
    $logFile = Join-Path $script:LogDir "$($script:TestName)-decode.log"
    $proc = Start-Process -FilePath "ffmpeg" -ArgumentList @("-hide_banner", "-v", "error", "-i", $File, "-f", "null", "-") `
        -RedirectStandardOutput $logFile -RedirectStandardError "$logFile.err" `
        -NoNewWindow -PassThru -Wait
    if ($proc.ExitCode -eq 0) {
        Pass-Check "Full decode test passed"
    } else {
        Fail-Check "Full decode test" "decode errors (see $logFile)"
    }
}

function Assert-LogContains {
    param([string]$File, [string]$Pattern, [string]$Label = "Log contains '$Pattern'")
    if ((Test-Path $File) -and (Select-String -Path $File -Pattern $Pattern -Quiet -ErrorAction SilentlyContinue)) {
        Pass-Check $Label
    } else {
        Fail-Check $Label "pattern '$Pattern' not found in $(Split-Path -Leaf $File)"
    }
}

function Assert-LogNotContains {
    param([string]$File, [string]$Pattern, [string]$Label = "Log does not contain '$Pattern'")
    if ((Test-Path $File) -and (Select-String -Path $File -Pattern $Pattern -Quiet -ErrorAction SilentlyContinue)) {
        Fail-Check $Label "pattern '$Pattern' was found in $(Split-Path -Leaf $File)"
    } else {
        Pass-Check $Label
    }
}

# ---- Result Reporting ----

function Report-Result {
    param([string]$Name = $script:TestName)
    Write-Host ""
    if ($script:ChecksFailed -eq 0) {
        Write-Host "RESULT: PASS - $Name ($($script:ChecksPassed) checks passed)" -ForegroundColor Green
        return 0
    } else {
        Write-Host "RESULT: FAIL - $Name ($($script:ChecksPassed) passed, $($script:ChecksFailed) failed)" -ForegroundColor Red
        Write-Host "  Server log: $($script:ServerLog)"
        return 1
    }
}
