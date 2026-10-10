param([string]$Go = 'go')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$savedEnv = @{ CGO_ENABLED = $env:CGO_ENABLED; GOOS = $env:GOOS; GOARCH = $env:GOARCH }
Push-Location $projectRoot
try {
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'
    $env:GOARCH = & $Go env GOHOSTARCH
    & $Go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Tests failed' }
    & $Go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go vet failed' }
    $unformatted = & (Join-Path (& $Go env GOROOT) 'bin/gofmt.exe') -l cmd internal
    if ($unformatted) { throw "gofmt required: $unformatted" }
    foreach ($arch in @('amd64', 'arm64')) {
        & "$PSScriptRoot/build.ps1" -Arch $arch -Version 'dev' -Go $Go
    }
} finally {
    foreach ($entry in $savedEnv.GetEnumerator()) {
        [Environment]::SetEnvironmentVariable($entry.Key, $entry.Value, 'Process')
    }
    Pop-Location
}
