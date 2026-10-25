$ErrorActionPreference = 'Stop'
$repo = Split-Path $PSScriptRoot -Parent
$bundle = Join-Path $repo 'dist/TokenTelemetryObservatory'
New-Item -ItemType Directory -Force $bundle | Out-Null
Push-Location (Join-Path $repo 'engine')
try {
    go build -trimpath -o (Join-Path $bundle 'observatory.exe') ./cmd/observatory
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }
    # Read-only Mac collectors, uploaded on demand by "Install Mac collector".
    foreach ($arch in @('arm64','amd64')) {
        $env:GOOS = 'darwin'; $env:GOARCH = $arch; $env:CGO_ENABLED = '0'
        go build -trimpath -o (Join-Path $bundle "collector-darwin-$arch") ./cmd/observatory-collect
        if ($LASTEXITCODE -ne 0) { throw "Mac collector ($arch) build failed" }
    }
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED
    go build -trimpath -o (Join-Path $repo 'dist/tokentelemetry.exe') ./cmd/tokentelemetry
    if ($LASTEXITCODE -ne 0) { throw 'Accounting CLI build failed' }
    go build -trimpath -o (Join-Path $repo 'dist/tt.exe') ./cmd/tt
    if ($LASTEXITCODE -ne 0) { throw 'Short CLI build failed' }
} finally { Pop-Location }
foreach ($name in @('Install.ps1','Launch.ps1','Stop.ps1','README.md')) { Copy-Item -LiteralPath (Join-Path $PSScriptRoot $name) -Destination $bundle -Force }
Copy-Item -LiteralPath (Join-Path $repo 'LICENSE') -Destination $bundle -Force
$zip = Join-Path $repo 'dist/TokenTelemetryObservatory-Windows-x64.zip'
Compress-Archive -Path (Join-Path $bundle '*') -DestinationPath $zip -Force
Get-FileHash -LiteralPath $zip -Algorithm SHA256 | Format-List
