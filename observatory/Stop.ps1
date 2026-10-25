$ErrorActionPreference = 'Stop'
$dataDir = Join-Path $env:LOCALAPPDATA 'TokenTelemetryObservatory'
$pidFile = Join-Path $dataDir 'server.pid'
$expectedExe = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot 'observatory.exe'))
# Ask the app to close its managed SSH tunnel before stopping the server.
try { Invoke-RestMethod 'http://127.0.0.1:8787/obs/disconnect' -Method Post -TimeoutSec 5 | Out-Null } catch { }
# Stop only processes running this installed executable, including copies not started by Launch.ps1.
$procs = @(Get-Process -Name 'observatory' -ErrorAction SilentlyContinue | Where-Object { $_.Path -and ([IO.Path]::GetFullPath($_.Path) -eq $expectedExe) })
foreach ($p in $procs) { Stop-Process -Id $p.Id -ErrorAction SilentlyContinue }
foreach ($p in $procs) { try { $p.WaitForExit(10000) | Out-Null } catch { } }
if (Test-Path -LiteralPath $pidFile) { Remove-Item -LiteralPath $pidFile }
Write-Output 'Observatory stopped. Configuration and counters are preserved.'
