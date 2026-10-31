param([switch]$SkipInstall, [switch]$SkipTests, [switch]$RunTests)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location $projectRoot
try {
    $goCommand = Get-Command go -ErrorAction SilentlyContinue
    $goExe = if ($goCommand) { $goCommand.Source } else { Join-Path $projectRoot '.tools\go\bin\go.exe' }
    if (-not (Test-Path -LiteralPath $goExe)) { throw '请先安装 Go 1.27.3 或运行 scripts\bootstrap-go.ps1' }
    if (-not (Get-Command npm.cmd -ErrorAction SilentlyContinue)) { throw '需要 Node.js 22.12+（已验证 24.20.0）及 npm。' }
    $env:GOPATH = Join-Path $projectRoot '.tools\gopath'
    $env:GOCACHE = Join-Path $projectRoot '.tools\gocache'
    $env:TMP = Join-Path $projectRoot '.tools\tmp'
    $env:TEMP = $env:TMP
    New-Item -ItemType Directory -Path $env:TMP,'bin' -Force | Out-Null
    if (-not $SkipInstall) {
        & npm.cmd ci --prefix frontend
        if ($LASTEXITCODE -ne 0) { throw 'npm ci 失败' }
    }
    if ($RunTests -and -not $SkipTests) {
        & $goExe test -count=1 -v ./...
        if ($LASTEXITCODE -ne 0) { throw 'Go 测试失败' }
        & npm.cmd test --prefix frontend
        if ($LASTEXITCODE -ne 0) { throw '前端测试失败' }
    }
    & npm.cmd run build --prefix frontend
    if ($LASTEXITCODE -ne 0) { throw 'TypeScript / Vite 构建失败' }
    & $goExe run -mod=readonly ./scripts/windows-resources
    if ($LASTEXITCODE -ne 0) { throw 'DeskGrid 图标与 Windows 产品资源生成失败' }
    & $goExe build -mod=readonly -trimpath -buildvcs=false -tags production -ldflags '-s -w -H=windowsgui' -o bin/DeskGrid.exe .
    if ($LASTEXITCODE -ne 0) { throw 'Windows 可执行文件构建失败（运行中的旧版本请先从托盘退出）。' }
    & (Join-Path $PSScriptRoot 'collect-licenses.ps1') -GoExe $goExe
    Get-FileHash -LiteralPath 'bin\DeskGrid.exe' -Algorithm SHA256 | Format-List
} finally { Pop-Location }
