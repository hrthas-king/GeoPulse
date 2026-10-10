param([string]$Go = 'go')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$savedEnv = @{ CGO_ENABLED = $env:CGO_ENABLED; GOOS = $env:GOOS; GOARCH = $env:GOARCH }
# 固定构建工具版本。它只在更换图标时运行，不进入应用运行依赖。
$resourceTool = 'github.com/tc-hib/go-winres@v0.3.3'
Push-Location $projectRoot
try {
    $hostOS = & $Go env GOHOSTOS
    $hostArch = & $Go env GOHOSTARCH
    if ($LASTEXITCODE -ne 0) { throw 'Cannot detect Go host architecture' }
    $env:CGO_ENABLED = '0'
    $env:GOOS = $hostOS
    $env:GOARCH = $hostArch

    # 从用户提供的原图生成多尺寸资源；按架构命名保证 Go 自动选择正确对象。
    & $Go run $resourceTool make --in assets/winres.json --arch amd64,arm64 --out cmd/geopulse/rsrc
    if ($LASTEXITCODE -ne 0) { throw 'Icon resource generation failed' }

    # 将资源链接到临时 EXE，再导出标准 ICO，保证 ICO 与程序资源完全一致。
    $stage = Join-Path $projectRoot '.artifacts/icon-build'
    New-Item -ItemType Directory -Force -Path $stage | Out-Null
    $temporaryExe = Join-Path $stage 'GeoPulse.exe'
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    & $Go build -trimpath -ldflags '-s -w -H=windowsgui' -o $temporaryExe ./cmd/geopulse
    if ($LASTEXITCODE -ne 0) { throw 'Temporary icon host build failed' }
    $env:GOOS = $hostOS
    $env:GOARCH = $hostArch
    & $Go run $resourceTool extract --dir $stage $temporaryExe
    if ($LASTEXITCODE -ne 0) { throw 'ICO export failed' }
    $icons = @(Get-ChildItem -LiteralPath $stage -Filter '*.ico' -File -Recurse)
    if ($icons.Count -ne 1) { throw 'Expected exactly one application icon' }
    Copy-Item -LiteralPath $icons[0].FullName -Destination assets/GeoPulse.ico -Force
    Get-Item -LiteralPath assets/GeoPulse.ico, cmd/geopulse/rsrc_windows_amd64.syso, cmd/geopulse/rsrc_windows_arm64.syso |
        Select-Object FullName, Length
} finally {
    foreach ($entry in $savedEnv.GetEnumerator()) {
        [Environment]::SetEnvironmentVariable($entry.Key, $entry.Value, 'Process')
    }
    Pop-Location
}
