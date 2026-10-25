param([switch]$NoBrowser)
$ErrorActionPreference = 'Stop'
$exe = Join-Path $PSScriptRoot 'observatory.exe'
$dataDir = Join-Path $env:LOCALAPPDATA 'TokenTelemetryObservatory'
New-Item -ItemType Directory -Force $dataDir | Out-Null
$url = 'http://127.0.0.1:8787'
$running = $false
try {
    $health = Invoke-RestMethod "$url/obs/health" -TimeoutSec 2
    $running = $health.application -eq 'TokenTelemetryObservatory'
} catch { }
if (-not $running) {
    $proc = Start-Process -FilePath $exe -ArgumentList @('-data-dir', ('"' + $dataDir + '"')) -WindowStyle Hidden -PassThru -RedirectStandardError (Join-Path $dataDir 'server.log')
    Set-Content -LiteralPath (Join-Path $dataDir 'server.pid') -Value $proc.Id
    for ($attempt = 0; $attempt -lt 30; $attempt++) {
        Start-Sleep -Milliseconds 300
        if ($proc.HasExited) { throw "Observatory exited. See $dataDir\server.log" }
        try {
            $health = Invoke-RestMethod "$url/obs/health" -TimeoutSec 1
            if ($health.application -eq 'TokenTelemetryObservatory') { $running = $true; break }
        } catch { }
    }
    if (-not $running) { throw "Observatory did not become ready. See $dataDir\server.log" }
}
if (-not $NoBrowser) { Start-Process $url }
Write-Output "Observatory is running at $url"
