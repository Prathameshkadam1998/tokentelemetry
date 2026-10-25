param([string]$SSHHost = '')
$ErrorActionPreference = 'Stop'
$destination = Join-Path $env:LOCALAPPDATA 'Programs/TokenTelemetryObservatory'
New-Item -ItemType Directory -Force $destination | Out-Null
# Stop a running copy first so Windows can replace the executable during an update.
& (Join-Path $PSScriptRoot 'Stop.ps1') -ErrorAction SilentlyContinue | Out-Null
$installedExe = [IO.Path]::GetFullPath((Join-Path $destination 'observatory.exe'))
Get-Process -Name 'observatory' -ErrorAction SilentlyContinue | Where-Object { $_.Path -and ([IO.Path]::GetFullPath($_.Path) -eq $installedExe) } | ForEach-Object { Stop-Process -Id $_.Id; $_.WaitForExit(10000) | Out-Null }
foreach ($name in @('observatory.exe', 'collector-darwin-arm64', 'collector-darwin-amd64', 'Launch.ps1', 'Stop.ps1', 'README.md', 'LICENSE')) {
    $source = Join-Path $PSScriptRoot $name
    $target = Join-Path $destination $name
    if ([IO.Path]::GetFullPath($source) -ne [IO.Path]::GetFullPath($target)) { Copy-Item -LiteralPath $source -Destination $target -Force }
}
$dataDir = Join-Path $env:LOCALAPPDATA 'TokenTelemetryObservatory'
New-Item -ItemType Directory -Force $dataDir | Out-Null
$configFile = Join-Path $dataDir 'config.json'
if ($SSHHost -and -not (Test-Path -LiteralPath $configFile)) {
    @{endpoint='http://127.0.0.1:11435';ssh_host=$SSHHost;tunnel=$true;min_tps=15;context_warning=80;cpu_warning=85;quota_warning=90;remote_history=$false} | ConvertTo-Json | Set-Content -LiteralPath $configFile -Encoding ascii
}
$shell = New-Object -ComObject WScript.Shell
$startMenu = Join-Path ([Environment]::GetFolderPath('Programs')) 'TokenTelemetry Observatory'
New-Item -ItemType Directory -Force $startMenu | Out-Null
foreach ($item in @(@('TokenTelemetry Observatory','Launch.ps1'),@('Stop Observatory','Stop.ps1'))) {
    $shortcut = $shell.CreateShortcut((Join-Path $startMenu ($item[0]+'.lnk')))
    $shortcut.TargetPath = Join-Path $env:SystemRoot 'System32/WindowsPowerShell/v1.0/powershell.exe'
    $shortcut.Arguments = '-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "' + (Join-Path $destination $item[1]) + '"'
    $shortcut.WorkingDirectory = $destination
    $shortcut.WindowStyle = 7
    $shortcut.Save()
}
Write-Output "Installed to $destination. Open TokenTelemetry Observatory from Start, or run Launch.ps1."
