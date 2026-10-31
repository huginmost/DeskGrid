param([switch]$RemoveConfig)
$ErrorActionPreference = 'Stop'
$installRoot = [IO.Path]::GetFullPath($PSScriptRoot)
$marker = Join-Path $installRoot '.deskgrid-install'
if (-not (Test-Path -LiteralPath $marker) -or (Get-Content -LiteralPath $marker -Raw).Trim() -ne 'deskgrid-1-current-user') { throw '请运行安装目录中的卸载脚本，未识别到 DeskGrid 安装标记。' }
if ($installRoot -eq [IO.Path]::GetPathRoot($installRoot)) { throw '拒绝操作磁盘根目录。' }
$binary = Join-Path $installRoot 'DeskGrid.exe'
if (Test-Path -LiteralPath $binary) { Start-Process -FilePath $binary -ArgumentList '--quit' -WindowStyle Hidden -Wait }
$waitUntil=(Get-Date).AddSeconds(8)
do {
    $running=@(Get-Process -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $binary })
    if (-not $running.Count) { break }
    Start-Sleep -Milliseconds 200
} while ((Get-Date) -lt $waitUntil)
if ($running.Count) { throw 'DeskGrid 仍有任务或未退出，请从托盘安全退出后重试卸载。' }
foreach ($base in @('HKCU:\Software\Classes\*\shell\DeskGrid.Pin','HKCU:\Software\Classes\Directory\shell\DeskGrid.Pin','HKCU:\Software\Classes\Directory\Background\shell\DeskGrid.Pin','HKCU:\Software\Classes\*\shell\Sticker.Pin','HKCU:\Software\Classes\Directory\shell\Sticker.Pin','HKCU:\Software\Classes\Directory\Background\shell\Sticker.Pin')) {
    if (Test-Path -LiteralPath ($base+'\command')) { Remove-Item -LiteralPath ($base+'\command') -Force }
    if (Test-Path -LiteralPath $base) { Remove-Item -LiteralPath $base -Force }
}
Remove-ItemProperty -LiteralPath 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' -Name 'DeskGrid' -ErrorAction SilentlyContinue
Remove-ItemProperty -LiteralPath 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' -Name 'Sticker' -ErrorAction SilentlyContinue
$uninstallKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\DeskGrid'
if (Test-Path -LiteralPath $uninstallKey) { Remove-Item -LiteralPath $uninstallKey -Force }
$shortcut = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\DeskGrid.lnk'
if (Test-Path -LiteralPath $shortcut) { Remove-Item -LiteralPath $shortcut -Force }
foreach ($name in @('DeskGrid.exe','README.md','THIRD_PARTY_NOTICES.md','.deskgrid-install','uninstall.ps1')) {
    $knownFile=Join-Path $installRoot $name
    if (Test-Path -LiteralPath $knownFile) { Remove-Item -LiteralPath $knownFile -Force }
}
$licenseRoot=[IO.Path]::GetFullPath((Join-Path $installRoot 'licenses'))
if ($licenseRoot -ne ($installRoot.TrimEnd('\')+'\licenses')) { throw '许可证目录校验失败' }
if (Test-Path -LiteralPath $licenseRoot) {
    if ((Get-Item -LiteralPath $licenseRoot -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw '拒绝递归清理重解析目录' }
    Remove-Item -LiteralPath $licenseRoot -Recurse -Force
}
if ($RemoveConfig) {
    $appDataRoot=[IO.Path]::GetFullPath($env:APPDATA).TrimEnd('\')
    $configRoot=[IO.Path]::GetFullPath((Join-Path $appDataRoot 'DeskGrid'))
    if (-not (Test-Path -LiteralPath $configRoot)) { $configRoot=[IO.Path]::GetFullPath((Join-Path $appDataRoot 'sticker')) }
    if ($configRoot -notin @(($appDataRoot+'\DeskGrid'),($appDataRoot+'\sticker'))) { throw '配置目录校验失败，未删除。' }
    if (Test-Path -LiteralPath $configRoot) {
        if ((Get-Item -LiteralPath $configRoot -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw '拒绝递归清理重解析目录' }
        Remove-Item -LiteralPath $configRoot -Recurse -Force
    }
}
if ((Get-ChildItem -LiteralPath $installRoot -Force | Measure-Object).Count -eq 0) { Remove-Item -LiteralPath $installRoot -Force }
Write-Output '已清理 DeskGrid 程序、自启与 Shell 集成。用户配置默认保留。'
