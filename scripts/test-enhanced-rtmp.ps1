# test-enhanced-rtmp.ps1 — Enhanced RTMP (H.265/HEVC) end-to-end test (Windows)
#
# PURPOSE:
#   Validates that the go-rtmp server correctly receives, processes, and records
#   an H.265/HEVC stream sent via Enhanced RTMP (E-RTMP v2). The test publishes
#   a synthetic test pattern from FFmpeg using the libx265 encoder, which triggers
#   Enhanced RTMP signaling (IsExHeader + FourCC "hvc1"). The server records the
#   stream to an FLV file, and the test verifies the recording preserves the
#   original codecs and content.
#
# PREREQUISITES:
#   - Go 1.21+ (to build the server)
#   - FFmpeg 6.1+ with libx265 encoder (Enhanced RTMP support)
#   - ffprobe (usually bundled with FFmpeg)
#   - ffplay (optional, for -Play mode)
#
# USAGE:
#   .\scripts\test-enhanced-rtmp.ps1           # Run automated test
#   .\scripts\test-enhanced-rtmp.ps1 -Play     # Run test + play recorded file
#
# WHAT IT TESTS:
#   1. Server accepts Enhanced RTMP connections with fourCcList negotiation
#   2. H.265 video is received and recorded without re-encoding (passthrough)
#   3. AAC audio is received and recorded correctly
#   4. Recorded FLV file is valid, decodable, and contains expected codecs
#
# VERIFICATION CHECKS (5 steps):
#   - Recorded file exists and is non-empty
#   - Video codec is HEVC (H.265)
#   - Audio codec is AAC
#   - Duration is within +/-2 seconds of source (5s)
#   - File is fully decodable (every frame decoded without errors)
#
# EXIT CODES:
#   0 - All checks passed
#   1 - One or more checks failed
#   2 - Missing prerequisites (FFmpeg too old, no libx265, etc.)

param(
    [switch]$Play  # After verification, play the recorded file with ffplay
)

$ErrorActionPreference = "Continue"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$ProjectRoot = Split-Path -Parent $ScriptDir
$LogDir = Join-Path $ScriptDir "logs"
$Binary = Join-Path $ProjectRoot "rtmp-server.exe"

# ===========================
# Configuration
# ===========================

$Port = 19370              # Unique port to avoid conflicts with test-e2e.ps1 (19351-19367)
$StreamKey = "live/etest"  # Stream key for this test
$SourceDuration = 5        # Seconds of test content to generate
$DurationTolerance = 2     # Acceptable duration drift (seconds)

# ===========================
# Process tracking for cleanup
# ===========================

$script:Pids = @()

function Cleanup {
    # Terminate all tracked processes on exit (normal, error, or interrupt).
    foreach ($pid in $script:Pids) {
        try {
            $proc = Get-Process -Id $pid -ErrorAction SilentlyContinue
            if ($proc -and -not $proc.HasExited) {
                Stop-Process -Id $pid -Force -ErrorAction SilentlyContinue
            }
        } catch {}
    }
    $script:Pids = @()
    # Remove temp directories created by this test.
    $tmpPath = Join-Path $ScriptDir ".test-tmp" "enhanced-rtmp"
    Remove-Item $tmpPath -Recurse -Force -ErrorAction SilentlyContinue
}

# Register cleanup handler for script exit and Ctrl+C.
$null = Register-EngineEvent -SourceIdentifier PowerShell.Exiting -Action { Cleanup }
trap { Cleanup }

# ===========================
# Helper functions
# ===========================

function Log($msg) {
    Write-Host "$(Get-Date -Format 'HH:mm:ss') $msg"
}

function Pass-Check($msg) {
    Write-Host "  PASS: $msg" -ForegroundColor Green
}

function Fail-Check($msg, $detail = "") {
    Write-Host "  FAIL: $msg" -ForegroundColor Red
    if ($detail) { Write-Host "        $detail" -ForegroundColor Red }
}

# Wait-ForLog polls a log file until a pattern appears or timeout is reached.
# Used to detect when the server is ready to accept connections.
function Wait-ForLog($file, $pattern, $timeout = 15) {
    for ($i = 0; $i -lt $timeout; $i++) {
        if ((Test-Path $file) -and (Select-String -Path $file -Pattern $pattern -Quiet -ErrorAction SilentlyContinue)) {
            return $true
        }
        Start-Sleep -Seconds 1
    }
    return $false
}

# ===========================
# Prerequisite checks
# ===========================

Write-Host "=== Enhanced RTMP (H.265) End-to-End Test ===" -ForegroundColor Cyan
Write-Host ""

# Check that FFmpeg is installed.
if (-not (Get-Command "ffmpeg" -ErrorAction SilentlyContinue)) {
    Write-Host "ERROR: ffmpeg not found. Install FFmpeg 6.1+ with libx265 support." -ForegroundColor Red
    exit 2
}

if (-not (Get-Command "ffprobe" -ErrorAction SilentlyContinue)) {
    Write-Host "ERROR: ffprobe not found. Install FFmpeg (includes ffprobe)." -ForegroundColor Red
    exit 2
}

# Verify FFmpeg has libx265 encoder (required for H.265 test content generation).
$encoders = & ffmpeg -hide_banner -encoders 2>&1 | Out-String
if ($encoders -notmatch "libx265") {
    Write-Host "ERROR: FFmpeg was built without libx265 encoder." -ForegroundColor Red
    Write-Host "       Install FFmpeg with H.265 support." -ForegroundColor Red
    exit 2
}

Log "Prerequisites OK: ffmpeg with libx265, ffprobe available"

# ===========================
# Build server
# ===========================

Log "Building server..."
Push-Location $ProjectRoot
& go build -o $Binary ./cmd/rtmp-server
if ($LASTEXITCODE -ne 0) {
    Pop-Location
    Write-Host "ERROR: Server build failed." -ForegroundColor Red
    exit 1
}
Pop-Location
Log "Built: $Binary"

# ===========================
# Prepare directories
# ===========================

New-Item -ItemType Directory -Path $LogDir -Force | Out-Null
$TmpDir = Join-Path $ScriptDir ".test-tmp" "enhanced-rtmp"
$RecordDir = Join-Path $TmpDir "recordings"
New-Item -ItemType Directory -Path $TmpDir -Force | Out-Null
New-Item -ItemType Directory -Path $RecordDir -Force | Out-Null

# ===========================
# Step 1: Start server with recording enabled
# ===========================

Write-Host ""
Log "Step 1: Starting server with recording (port $Port)"

$ServerLog = Join-Path $LogDir "test-enhanced-rtmp-server.log"
$ServerErr = Join-Path $LogDir "test-enhanced-rtmp-server-err.log"

# Start the RTMP server with:
#   -record-all     : Record every published stream to FLV
#   -record-dir     : Write recordings to our temp directory
#   -log-level debug: Capture Enhanced RTMP negotiation details in logs
$serverProc = Start-Process -FilePath $Binary -ArgumentList @(
    "-listen", ":$Port",
    "-record-all",
    "-record-dir", $RecordDir,
    "-log-level", "debug"
) -RedirectStandardOutput $ServerLog -RedirectStandardError $ServerErr `
  -NoNewWindow -PassThru

$script:Pids += $serverProc.Id

# Wait for the server to be ready (look for startup log message).
if (-not (Wait-ForLog $ServerLog "server started|server listening" 10)) {
    Write-Host "ERROR: Server failed to start. Log:" -ForegroundColor Red
    if (Test-Path $ServerLog) { Get-Content $ServerLog | Select-Object -Last 20 | Write-Host }
    if (Test-Path $ServerErr) { Get-Content $ServerErr | Select-Object -Last 20 | Write-Host }
    exit 1
}

Log "Server started (PID $($serverProc.Id))"

# ===========================
# Step 2: Publish H.265 stream via Enhanced RTMP
# ===========================

Write-Host ""
Log "Step 2: Publishing H.265+AAC test stream via Enhanced RTMP"

$PublishLog = Join-Path $LogDir "test-enhanced-rtmp-publish.log"

# Generate a synthetic test pattern and encode it as H.265 (HEVC) + AAC.
# When FFmpeg 6.1+ writes H.265 to the FLV muxer, it automatically uses
# Enhanced RTMP signaling (IsExHeader=1, FourCC="hvc1") instead of the
# legacy CodecID field.
#
# Source:  testsrc2 (video) + sine (audio) - no input file needed
# Video:  libx265, ultrafast preset (fast encoding for tests)
# Audio:  AAC, 64 kbps
# Output: RTMP to the local server
$pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "warning",
    "-f", "lavfi", "-i", "testsrc2=duration=${SourceDuration}:size=640x480:rate=30",
    "-f", "lavfi", "-i", "sine=frequency=440:duration=${SourceDuration}",
    "-c:v", "libx265", "-preset", "ultrafast",
    "-c:a", "aac", "-b:a", "64k",
    "-f", "flv", "rtmp://localhost:${Port}/${StreamKey}"
) -NoNewWindow -PassThru -Wait -RedirectStandardError $PublishLog

# Allow time for the server to flush the recording to disk.
Start-Sleep -Seconds 2

Log "Publish complete"

# ===========================
# Step 3: Stop the server
# ===========================

Log "Stopping server..."
try {
    $p = Get-Process -Id $serverProc.Id -ErrorAction SilentlyContinue
    if ($p -and -not $p.HasExited) {
        Stop-Process -Id $serverProc.Id -Force -ErrorAction SilentlyContinue
        Start-Sleep -Milliseconds 500
    }
} catch {}
$script:Pids = $script:Pids | Where-Object { $_ -ne $serverProc.Id }

# ===========================
# Step 4: Find the recorded FLV file
# ===========================

Write-Host ""
Log "Step 3: Verifying recorded file"

# The recorder saves files as: {streamkey_with_slashes_replaced}_{timestamp}.flv
# For stream key "live/etest", the file is "live_etest_YYYYMMDD_HHMMSS.flv"
$RecordedFile = Get-ChildItem -Path $RecordDir -Filter "live_etest_*.flv" -ErrorAction SilentlyContinue | Select-Object -First 1

# ===========================
# Step 5: Verification checks
# ===========================

$ChecksPassed = 0
$ChecksFailed = 0

# --- Check 1: File exists and is non-empty ---
if (-not $RecordedFile -or $RecordedFile.Length -eq 0) {
    Fail-Check "File exists and is non-empty" "No recording found in $RecordDir"
    $ChecksFailed++

    # Show server log tail to diagnose why recording failed.
    Write-Host ""
    Write-Host "Server log (last 30 lines):" -ForegroundColor Yellow
    if (Test-Path $ServerLog) { Get-Content $ServerLog | Select-Object -Last 30 | Write-Host }
    if (Test-Path $ServerErr) { Get-Content $ServerErr | Select-Object -Last 30 | Write-Host }
    Write-Host ""
    Write-Host "Publish log:" -ForegroundColor Yellow
    if (Test-Path $PublishLog) { Get-Content $PublishLog | Write-Host }
    Write-Host ""
    Write-Host "RESULT: FAIL - No recorded file produced" -ForegroundColor Red
    exit 1
}

Pass-Check "File exists and is non-empty ($($RecordedFile.Name), $($RecordedFile.Length) bytes)"
$ChecksPassed++

# --- Check 2: Video codec is HEVC ---
# Use ffprobe to extract the video codec name from the recorded FLV.
$VideoCodec = ""
try {
    $VideoCodec = (& ffprobe -v error -select_streams v:0 `
        -show_entries stream=codec_name -of csv=p=0 `
        $RecordedFile.FullName 2>&1).Trim()
} catch {}

if ($VideoCodec -eq "hevc") {
    Pass-Check "Video codec is HEVC (got: $VideoCodec)"
    $ChecksPassed++
} else {
    Fail-Check "Video codec is HEVC" "got: '$VideoCodec'"
    $ChecksFailed++
}

# --- Check 3: Audio codec is AAC ---
$AudioCodec = ""
try {
    $AudioCodec = (& ffprobe -v error -select_streams a:0 `
        -show_entries stream=codec_name -of csv=p=0 `
        $RecordedFile.FullName 2>&1).Trim()
} catch {}

if ($AudioCodec -eq "aac") {
    Pass-Check "Audio codec is AAC (got: $AudioCodec)"
    $ChecksPassed++
} else {
    Fail-Check "Audio codec is AAC" "got: '$AudioCodec'"
    $ChecksFailed++
}

# --- Check 4: Duration within tolerance ---
# The source duration is 5 seconds; allow +/-2s for streaming/recording overhead.
$RecordedDuration = 0
try {
    $durationStr = (& ffprobe -v error -show_entries format=duration `
        -of csv=p=0 $RecordedFile.FullName 2>&1).Trim()
    $RecordedDuration = [double]$durationStr
} catch {}

$DurationLo = $SourceDuration - $DurationTolerance
$DurationHi = $SourceDuration + $DurationTolerance

if ($RecordedDuration -ge $DurationLo -and $RecordedDuration -le $DurationHi) {
    Pass-Check "Duration within tolerance (${RecordedDuration}s, expected ~${SourceDuration}s +/-${DurationTolerance}s)"
    $ChecksPassed++
} else {
    Fail-Check "Duration within tolerance" "got ${RecordedDuration}s, expected ${SourceDuration}s +/-${DurationTolerance}s"
    $ChecksFailed++
}

# --- Check 5: Full decode test ---
# Decode the entire recorded file to null. If any frame is corrupted or
# the container is malformed, ffmpeg will exit with a non-zero status.
$DecodeLog = Join-Path $LogDir "test-enhanced-rtmp-decode.log"
& ffmpeg -hide_banner -v error -i $RecordedFile.FullName -f null NUL 2>$DecodeLog

if ($LASTEXITCODE -eq 0) {
    Pass-Check "Full decode test passed (all frames decodable)"
    $ChecksPassed++
} else {
    Fail-Check "Full decode test" "ffmpeg decode errors (see $DecodeLog)"
    $ChecksFailed++
}

# ===========================
# Results summary
# ===========================

Write-Host ""
Write-Host "=== Results ===" -ForegroundColor Cyan
Write-Host "  Checks passed: $ChecksPassed" -ForegroundColor Green
Write-Host "  Checks failed: $ChecksFailed" -ForegroundColor Red

# Show Enhanced RTMP negotiation from server log (informational).
if (Test-Path $ServerLog) {
    $enhancedLines = Select-String -Path $ServerLog -Pattern "enhanced|fourcc|hvc1|hevc|H265" -ErrorAction SilentlyContinue
    if ($enhancedLines) {
        Write-Host ""
        Write-Host "Enhanced RTMP activity in server log:" -ForegroundColor Cyan
        $enhancedLines | Select-Object -First 10 | ForEach-Object { Write-Host "  $($_.Line)" }
    }
}

# ===========================
# Optional: play the recorded file
# ===========================

if ($Play -and $ChecksFailed -eq 0) {
    Write-Host ""
    if (Get-Command "ffplay" -ErrorAction SilentlyContinue) {
        Log "Playing recorded file with ffplay (close window to exit)..."
        & ffplay -autoexit -window_title "Enhanced RTMP H.265 Recording" $RecordedFile.FullName 2>$null
    } else {
        Write-Host "ffplay not found - skipping playback" -ForegroundColor Yellow
    }
}

# ===========================
# Final exit
# ===========================

Write-Host ""
if ($ChecksFailed -gt 0) {
    Write-Host "RESULT: FAIL - $ChecksFailed check(s) failed" -ForegroundColor Red
    Write-Host "  Server log: $ServerLog"
    Write-Host "  Publish log: $PublishLog"
    exit 1
} else {
    Write-Host "RESULT: PASS - Enhanced RTMP H.265 end-to-end test succeeded" -ForegroundColor Green
    exit 0
}
