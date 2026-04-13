# run-secure-windows.ps1
# Generates self-signed TLS certificates (if missing) and starts RTMP + RTMPS.
param(
    [int]$Port = 1935,
    [int]$TLSPort = 1936,
    [ValidateSet("debug", "info", "warn", "error")]
    [string]$LogLevel = "debug"
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition

& "$ScriptDir\start-server.ps1" `
    -Mode both `
    -Port $Port `
    -TLSPort $TLSPort `
    -LogLevel $LogLevel `
    -Foreground
