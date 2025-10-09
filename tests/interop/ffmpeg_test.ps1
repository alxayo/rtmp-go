<#
FFmpeg / ffplay interoperability automated test script (T054)

Prerequisites:
  - PowerShell 5+ (Windows default)
  - go toolchain installed and on PATH
  - ffmpeg & ffplay installed and on PATH (https://ffmpeg.org/)

Tests Implemented:
  1. PublishOnly        : Start server, publish single stream
  2. PublishAndPlay     : Start server, publish then play with ffplay
  3. Concurrency        : Two parallel publishers + two parallel players
  4. Recording          : Start server with -record-all, verify FLV recorded & playable

Usage Examples:
  ./ffmpeg_test.ps1                     # run all tests
  ./ffmpeg_test.ps1 -Include PublishOnly,Recording
  ./ffmpeg_test.ps1 -FFmpegPath "C:\\tools\\ffmpeg" -ServerFlags "-log-level debug"

Exit Codes:
  0 success, non‑zero indicates number of failed tests.

Generates a temporary working directory under $env:TEMP/go-rtmp-interop
Cleans up child processes on exit (best effort).
#>
[CmdletBinding()]
param(
    [string[]]$Include = @('PublishOnly','PublishAndPlay','Concurrency','Recording'),
    [string]$FFmpegExe = 'ffmpeg',
    [string]$FFplayExe = 'ffplay',
    [int]$ServerPort = 1935,
    [string]$ServerFlags = '',
    [int]$TimeoutSeconds = 45,
    [switch]$KeepWorkDir
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Assert-CmdExists($cmd) {
  if (-not (Get-Command $cmd -ErrorAction SilentlyContinue)) {
    throw "Required command '$cmd' not found on PATH"
  }
}

Assert-CmdExists go
Assert-CmdExists $FFmpegExe
Assert-CmdExists $FFplayExe

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..' '..' '..')).Path
$ServerBin = Join-Path $Root 'rtmp-server.exe'
if (-not (Test-Path $ServerBin)) {
  Write-Host 'Building server binary...'
  Push-Location $Root
  go build -o rtmp-server.exe ./cmd/rtmp-server
  Pop-Location
}

$Work = Join-Path $env:TEMP ("go-rtmp-interop-" + [guid]::NewGuid())
New-Item -ItemType Directory -Force -Path $Work | Out-Null
$RecordingDir = Join-Path $Work 'recordings'
New-Item -ItemType Directory -Force -Path $RecordingDir | Out-Null

Write-Host "Work directory: $Work" -ForegroundColor Cyan

$global:Failures = @()
$Procs = @()

function Start-Server([string]$ExtraFlags) {
  $args = @('-listen', ":$ServerPort")
  if ($ExtraFlags) { $args += $ExtraFlags.Split(' ') }
  Write-Host "Starting server: $ServerBin $args" -ForegroundColor DarkCyan
  $p = Start-Process -FilePath $ServerBin -ArgumentList $args -WorkingDirectory $Root -PassThru -NoNewWindow
  $Procs += $p
  Start-Sleep -Milliseconds 600
  return $p
}

function Stop-All {
  foreach ($p in $Procs) { if (!$p.HasExited) { try { $p.CloseMainWindow() | Out-Null } catch {}; try { $p.Kill() } catch {} } }
}

Register-EngineEvent PowerShell.Exiting -Action { Stop-All } | Out-Null

function New-DummyMP4($path) {
  if (Test-Path $path) { return }
  Write-Host 'Generating synthetic test.mp4 (1s color / tone)' -ForegroundColor DarkGray
  & $FFmpegExe -hide_banner -loglevel error -y -f lavfi -i testsrc=size=640x360:rate=30 -f lavfi -i sine=frequency=1000:sample_rate=44100 -t 1 -c:v libx264 -pix_fmt yuv420p -c:a aac $path
}

$TestMedia = Join-Path $Work 'test.mp4'
New-DummyMP4 $TestMedia

function Invoke-FFmpegPublish([string]$Url,[string]$Input,[switch]$Quiet){
  $pubArgs = @('-hide_banner','-loglevel','error','-re','-i', $Input,'-c','copy','-f','flv', $Url)
  if ($Quiet) { $pubArgs = @('-hide_banner','-loglevel','error','-nostdin','-re','-i', $Input,'-c','copy','-f','flv', $Url) }
  Write-Host "Publishing -> $Url" -ForegroundColor Green
  & $FFmpegExe @pubArgs
  return $LASTEXITCODE
}

function Test-PublishOnly {
  $name = 'PublishOnly'
  Write-Host "=== $name ===" -ForegroundColor Yellow
  $server = Start-Server $ServerFlags
  try {
    $url = "rtmp://localhost:$ServerPort/live/test"
    $code = Invoke-FFmpegPublish -Url $url -Input $TestMedia -Quiet
    if ($code -ne 0) { throw "ffmpeg exited $code" }
  } catch { $global:Failures += $name; Write-Error $_ } finally { Stop-All }
}

function Test-PublishAndPlay {
  $name = 'PublishAndPlay'
  Write-Host "=== $name ===" -ForegroundColor Yellow
  $server = Start-Server $ServerFlags
  $playLog = Join-Path $Work 'ffplay.log'
  try {
    $url = "rtmp://localhost:$ServerPort/live/play1"
    $pubJob = Start-Job { param($ff,$in,$u) & $ff -hide_banner -loglevel error -re -i $in -c copy -f flv $u } -ArgumentList $FFmpegExe,$TestMedia,$url
    Start-Sleep -Milliseconds 800
    $ffplayArgs = @('-hide_banner','-loglevel','error','-autoexit',"rtmp://localhost:$ServerPort/live/play1")
    Write-Host 'Starting ffplay (headless)...' -ForegroundColor Green
    # Use -analyzeduration short to exit quickly after playback
    $p = Start-Process -FilePath $FFplayExe -ArgumentList $ffplayArgs -RedirectStandardError $playLog -RedirectStandardOutput $playLog -NoNewWindow -PassThru
    $Procs += $p
    $wait = $p.WaitForExit(10000)
    if (-not $wait) { throw 'ffplay timeout' }
    Receive-Job $pubJob | Out-Null
    if ($p.ExitCode -ne 0) { throw "ffplay exit code $($p.ExitCode)" }
  } catch { $global:Failures += $name; Write-Error $_ } finally { Stop-All }
}

function Test-Concurrency {
  $name = 'Concurrency'
  Write-Host "=== $name ===" -ForegroundColor Yellow
  $server = Start-Server $ServerFlags
  try {
    $pub1 = Start-Job { param($ff,$in,$url) & $ff -hide_banner -loglevel error -re -i $in -c copy -f flv $url } -ArgumentList $FFmpegExe,$TestMedia,"rtmp://localhost:$ServerPort/live/a"
    $pub2 = Start-Job { param($ff,$in,$url) & $ff -hide_banner -loglevel error -re -i $in -c copy -f flv $url } -ArgumentList $FFmpegExe,$TestMedia,"rtmp://localhost:$ServerPort/live/b"
    Start-Sleep -Milliseconds 1000
    $play1 = Start-Job { param($fp,$url) & $fp -hide_banner -loglevel error -autoexit $url } -ArgumentList $FFplayExe,"rtmp://localhost:$ServerPort/live/a"
    $play2 = Start-Job { param($fp,$url) & $fp -hide_banner -loglevel error -autoexit $url } -ArgumentList $FFplayExe,"rtmp://localhost:$ServerPort/live/b"
    Wait-Job -Job $pub1,$pub2,$play1,$play2 -Timeout 20 | Out-Null
    foreach ($j in $pub1,$pub2,$play1,$play2) {
      if ($j.State -ne 'Completed') { throw "Job $($j.Id) not completed" }
      $code = (Receive-Job $j -ErrorAction SilentlyContinue; $LASTEXITCODE)
    }
  } catch { $global:Failures += $name; Write-Error $_ } finally { Stop-All }
}

function Test-Recording {
  $name = 'Recording'
  Write-Host "=== $name ===" -ForegroundColor Yellow
  $flags = "$ServerFlags -record-all -record-dir $RecordingDir"
  $server = Start-Server $flags
  try {
    $url = "rtmp://localhost:$ServerPort/live/rec1"
    $code = Invoke-FFmpegPublish -Url $url -Input $TestMedia -Quiet
    if ($code -ne 0) { throw "ffmpeg exited $code" }
    Stop-All
    $flv = Get-ChildItem -Path $RecordingDir -Filter *.flv | Sort-Object LastWriteTime -Descending | Select-Object -First 1
    if (-not $flv) { throw 'No FLV recording found' }
    Write-Host "Verifying recording: $($flv.Name)" -ForegroundColor Green
    & $FFmpegExe -hide_banner -loglevel error -i $flv.FullName -f null - | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Recorded FLV not decodable' }
  } catch { $global:Failures += $name; Write-Error $_ } finally { Stop-All }
}

$Disp = @{
  'PublishOnly'     = { Test-PublishOnly }
  'PublishAndPlay'  = { Test-PublishAndPlay }
  'Concurrency'     = { Test-Concurrency }
  'Recording'       = { Test-Recording }
}

foreach ($t in $Include) {
  if ($Disp.ContainsKey($t)) { & $Disp[$t] } else { Write-Warning "Unknown test '$t'" }
}

if ($Failures.Count -gt 0) {
  Write-Host ("FAILED: " + ($Failures -join ', ')) -ForegroundColor Red
  if (-not $KeepWorkDir) { Remove-Item -Recurse -Force $Work }
  exit $Failures.Count
} else {
  Write-Host 'All interop tests passed.' -ForegroundColor Green
  if (-not $KeepWorkDir) { Remove-Item -Recurse -Force $Work }
  exit 0
}