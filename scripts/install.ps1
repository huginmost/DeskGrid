param([string]$Destination = (Join-Path $env:LOCALAPPDATA 'Programs\DeskGrid'))
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$binary = Join-Path $projectRoot 'bin\DeskGrid.exe'
if (-not (Test-Path -LiteralPath $binary)) { $binary = Join-Path $projectRoot 'DeskGrid.exe' }
if (-not (Test-Path -LiteralPath $binary)) { throw '请先运行 build.ps1。' }
$installRoot = [IO.Path]::GetFullPath($Destination)
if ($installRoot -eq [IO.Path]::GetPathRoot($installRoot)) { throw '不能安装到磁盘根目录。' }
New-Item -ItemType Directory -Path $installRoot -Force | Out-Null
Copy-Item -LiteralPath $binary -Destination (Join-Path $installRoot 'DeskGrid.exe') -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'README.md') -Destination $installRoot -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'THIRD_PARTY_NOTICES.md') -Destination $installRoot -Force
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'uninstall.ps1') -Destination $installRoot -Force
$licenseRoot = Join-Path $projectRoot 'bin\licenses'
if (-not (Test-Path -LiteralPath $licenseRoot)) { $licenseRoot = Join-Path $projectRoot 'licenses' }
if (Test-Path -LiteralPath $licenseRoot) { Copy-Item -LiteralPath $licenseRoot -Destination $installRoot -Recurse -Force }
Set-Content -LiteralPath (Join-Path $installRoot '.deskgrid-install') -Value 'deskgrid-1-current-user' -Encoding utf8
$menuRoot = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs'
$shortcut = (New-Object -ComObject WScript.Shell).CreateShortcut((Join-Path $menuRoot 'DeskGrid.lnk'))
$shortcut.TargetPath = Join-Path $installRoot 'DeskGrid.exe'
$shortcut.WorkingDirectory = $installRoot
$shortcut.Save()
$key = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\DeskGrid'
New-Item -Path $key -Force | Out-Null
$values = @{DisplayName='DeskGrid';DisplayVersion='1.0.1';Publisher='DeskGrid';InstallLocation=$installRoot;DisplayIcon=(Join-Path $installRoot 'DeskGrid.exe');UninstallString=('powershell.exe -NoProfile -ExecutionPolicy Bypass -File "' + (Join-Path $installRoot 'uninstall.ps1') + '"')}
foreach ($name in $values.Keys) { Set-ItemProperty -LiteralPath $key -Name $name -Value $values[$name] }
Write-Output "已安装到 $installRoot。自启和资源管理器集成请在 DeskGrid 设置中按需开启。"
