param([string]$SSHHost = '')
$ErrorActionPreference = 'Stop'
$destination = Join-Path $env:LOCALAPPDATA 'Programs/TokenTelemetryObservatory'
New-Item -ItemType Directory -Force $destination | Out-Null
# Stop a running copy first so Windows can replace the executable during an update.
& (Join-Path $PSScriptRoot 'Stop.ps1') -ErrorAction SilentlyContinue | Out-Null
$installedExe = [IO.Path]::GetFullPath((Join-Path $destination 'observatory.exe'))
Get-Process -Name 'observatory' -ErrorAction SilentlyContinue | Where-Object { $_.Path -and ([IO.Path]::GetFullPath($_.Path) -eq $installedExe) } | ForEach-Object { Stop-Process -Id $_.Id; $_.WaitForExit(10000) | Out-Null }
foreach ($name in @('observatory.exe', 'collector-darwin-arm64', 'collector-darwin-amd64', 'Launch.ps1', 'Stop.ps1', 'Uninstall.ps1', 'README.md', 'LICENSE')) {
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
$powershell = Join-Path $env:SystemRoot 'System32/WindowsPowerShell/v1.0/powershell.exe'
function New-ObservatoryShortcut([string]$path, [string]$script, [string]$description) {
    $shortcut = $shell.CreateShortcut($path)
    $shortcut.TargetPath = $powershell
    $shortcut.Arguments = '-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "' + (Join-Path $destination $script) + '"'
    $shortcut.WorkingDirectory = $destination
    $shortcut.WindowStyle = 7
    $shortcut.IconLocation = (Join-Path $destination 'observatory.exe') + ',0'
    $shortcut.Description = $description
    $shortcut.Save()
}
$programs = [Environment]::GetFolderPath('Programs')
$startMenu = Join-Path $programs 'TokenTelemetry Observatory'
New-Item -ItemType Directory -Force $startMenu | Out-Null
New-ObservatoryShortcut (Join-Path $startMenu 'TokenTelemetry Observatory.lnk') 'Launch.ps1' 'Open the TokenTelemetry Observatory dashboard'
New-ObservatoryShortcut (Join-Path $startMenu 'Stop Observatory.lnk') 'Stop.ps1' 'Stop the TokenTelemetry Observatory background server'
New-ObservatoryShortcut (Join-Path $startMenu 'Uninstall Observatory.lnk') 'Uninstall.ps1' 'Remove TokenTelemetry Observatory'
# Top-level Start menu entry (searchable) and a Desktop icon.
New-ObservatoryShortcut (Join-Path $programs 'TokenTelemetry Observatory.lnk') 'Launch.ps1' 'Open the TokenTelemetry Observatory dashboard'
New-ObservatoryShortcut (Join-Path ([Environment]::GetFolderPath('Desktop')) 'TokenTelemetry Observatory.lnk') 'Launch.ps1' 'Open the TokenTelemetry Observatory dashboard'
# Per-user entry in Settings > Apps > Installed apps (no admin rights needed).
$uninstallKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\TokenTelemetryObservatory'
New-Item -Path $uninstallKey -Force | Out-Null
$uninstallCommand = '"' + $powershell + '" -NoProfile -ExecutionPolicy Bypass -File "' + (Join-Path $destination 'Uninstall.ps1') + '"'
$values = @{ DisplayName = 'TokenTelemetry Observatory'; DisplayVersion = '0.2.0'; Publisher = 'Prathamesh Kadam (based on TokenTelemetry, MIT)';
    InstallLocation = $destination; DisplayIcon = (Join-Path $destination 'observatory.exe'); UninstallString = $uninstallCommand;
    URLInfoAbout = 'https://github.com/Prathameshkadam1998/tokentelemetry' }
foreach ($k in $values.Keys) { Set-ItemProperty -Path $uninstallKey -Name $k -Value $values[$k] }
Set-ItemProperty -Path $uninstallKey -Name NoModify -Value 1 -Type DWord
Set-ItemProperty -Path $uninstallKey -Name NoRepair -Value 1 -Type DWord
Write-Output "Installed to $destination. Open TokenTelemetry Observatory from the Desktop or Start menu."
