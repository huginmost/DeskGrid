param()
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$binary = Join-Path $projectRoot 'bin\DeskGrid.exe'
if (-not (Test-Path -LiteralPath $binary)) { throw '请先运行 scripts\build.ps1。' }
$stage = Join-Path $projectRoot ('.tools\deskgrid-package-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
$packageRoot = Join-Path $stage 'DeskGrid'
New-Item -ItemType Directory -Path $packageRoot -Force | Out-Null
Copy-Item -LiteralPath $binary -Destination $packageRoot
foreach ($name in @('README.md','bugfix.md','THIRD_PARTY_NOTICES.md')) {
    Copy-Item -LiteralPath (Join-Path $projectRoot $name) -Destination $packageRoot
}
Copy-Item -LiteralPath (Join-Path $projectRoot 'bin\licenses') -Destination $packageRoot -Recurse
New-Item -ItemType Directory -Path (Join-Path $packageRoot 'scripts'),(Join-Path $packageRoot 'docs'),(Join-Path $packageRoot 'internal\branding') -Force | Out-Null
Copy-Item -LiteralPath (Join-Path $projectRoot 'internal\branding\deskgrid.png') -Destination (Join-Path $packageRoot 'internal\branding')
foreach ($name in @('install.ps1','uninstall.ps1')) {
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot $name) -Destination (Join-Path $packageRoot 'scripts')
}
# install.ps1 expects the project's bin directory; retain a single distributable
# EXE at the package root by making the installer accept either layout.
foreach ($name in @('protocol.md','BUILDING.md','RELEASE_NOTES.md','build-package.txt')) {
    Copy-Item -LiteralPath (Join-Path $projectRoot ('docs\'+$name)) -Destination (Join-Path $packageRoot 'docs')
}
$outputRoot = Join-Path $projectRoot 'release'
New-Item -ItemType Directory -Path $outputRoot -Force | Out-Null
$zip = Join-Path $outputRoot 'DeskGrid-windows-amd64.zip'
Compress-Archive -LiteralPath $packageRoot -DestinationPath $zip -Force
Copy-Item -LiteralPath $packageRoot -Destination $outputRoot -Recurse -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'docs\RELEASE_NOTES.md') -Destination (Join-Path $outputRoot 'RELEASE_NOTES.md') -Force
$exeHash = (Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash.ToLowerInvariant()
$zipHash = (Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash.ToLowerInvariant()
Set-Content -LiteralPath (Join-Path $outputRoot 'SHA256SUMS.txt') -Value @(($zipHash+' *DeskGrid-windows-amd64.zip'), ($exeHash+' *DeskGrid/DeskGrid.exe')) -Encoding ascii
Get-FileHash -LiteralPath $binary,$zip -Algorithm SHA256 | Format-Table Path,Hash -AutoSize
