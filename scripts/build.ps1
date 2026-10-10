param(
    [ValidateSet('amd64', 'arm64')][string]$Arch = 'amd64',
    [ValidatePattern('^[A-Za-z0-9][A-Za-z0-9.+_-]*$')][string]$Version = 'dev',
    [string]$Go = 'go'
)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$savedEnv = @{ CGO_ENABLED = $env:CGO_ENABLED; GOOS = $env:GOOS; GOARCH = $env:GOARCH }
Push-Location $projectRoot
try {
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'
    $env:GOARCH = $Arch
    if (-not (Test-Path -LiteralPath "cmd/geopulse/rsrc_windows_$Arch.syso")) {
        throw 'Missing application icon resource. Run scripts/generate-icons.ps1 first.'
    }
    New-Item -ItemType Directory -Force -Path dist | Out-Null
    $target = "dist/GeoPulse-windows-$Arch.exe"
    & $Go build -trimpath -ldflags "-s -w -H=windowsgui -X main.version=$Version" -o $target ./cmd/geopulse
    if ($LASTEXITCODE -ne 0) { throw "Go build failed ($LASTEXITCODE)" }
    Get-Item -LiteralPath $target | Select-Object FullName, Length
} finally {
    foreach ($entry in $savedEnv.GetEnumerator()) {
        [Environment]::SetEnvironmentVariable($entry.Key, $entry.Value, 'Process')
    }
    Pop-Location
}
