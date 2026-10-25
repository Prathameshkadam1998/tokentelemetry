# Removes the app, shortcuts and Installed-apps entry. Settings and counters in
# %LOCALAPPDATA%\TokenTelemetryObservatory are kept unless -RemoveData is given.
param([switch]$RemoveData)
$ErrorActionPreference = 'Continue'
$destination = Join-Path $env:LOCALAPPDATA 'Programs\TokenTelemetryObservatory'
$stop = Join-Path $destination 'Stop.ps1'
if (Test-Path -LiteralPath $stop) { & $stop | Out-Null }
$programs = [Environment]::GetFolderPath('Programs')
Remove-Item -LiteralPath (Join-Path $programs 'TokenTelemetry Observatory') -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath (Join-Path $programs 'TokenTelemetry Observatory.lnk') -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath (Join-Path ([Environment]::GetFolderPath('Desktop')) 'TokenTelemetry Observatory.lnk') -Force -ErrorAction SilentlyContinue
Remove-Item -Path 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\TokenTelemetryObservatory' -Recurse -Force -ErrorAction SilentlyContinue
if ($RemoveData) { Remove-Item -LiteralPath (Join-Path $env:LOCALAPPDATA 'TokenTelemetryObservatory') -Recurse -Force -ErrorAction SilentlyContinue }
# The running script lives in the folder being removed, so delete it after this process exits.
Start-Process -FilePath 'cmd.exe' -ArgumentList '/c', ('timeout /t 2 >nul & rmdir /s /q "' + $destination + '"') -WindowStyle Hidden
Write-Output 'TokenTelemetry Observatory was uninstalled.'
