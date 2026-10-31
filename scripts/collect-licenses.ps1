param([Parameter(Mandatory=$true)][string]$GoExe)
$ErrorActionPreference='Stop'
$projectRoot=Split-Path -Parent $PSScriptRoot
$licenseRoot=Join-Path $projectRoot 'bin\licenses'
New-Item -ItemType Directory -Path $licenseRoot -Force | Out-Null
$modules=& $GoExe list -mod=readonly -deps -f '{{if not .Standard}}{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}{{end}}' .
if ($LASTEXITCODE -ne 0) { throw '无法读取 Go 依赖许可证位置' }
foreach ($line in ($modules | Sort-Object -Unique)) {
    $parts=$line.Split('|')
    if ($parts.Count -ne 3 -or -not $parts[2] -or -not $parts[1]) { continue }
    $name=[Regex]::Replace(($parts[0]+'@'+$parts[1]),'[^A-Za-z0-9_.@-]','_')
    $files=Get-ChildItem -LiteralPath $parts[2] -File | Where-Object { $_.Name -match '^(LICENSE|COPYING|NOTICE)' }
    foreach ($file in $files) { Copy-Item -LiteralPath $file.FullName -Destination (Join-Path $licenseRoot ($name+'--'+$file.Name)) -Force }
}
$packageJson=& node -e 'const p=require(process.argv[1]).packages; console.log(JSON.stringify(Object.entries(p).filter(([k,v])=>k&&!v.dev).map(([path,p])=>({path,version:p.version}))));' (Join-Path $projectRoot 'frontend\package-lock.json')
if ($LASTEXITCODE -ne 0) { throw '无法读取 npm 许可证清单' }
$packages=$packageJson | ConvertFrom-Json
foreach ($entry in $packages) {
    if (-not $entry.path.StartsWith('node_modules/')) { continue }
    $packageDir=Join-Path (Join-Path $projectRoot 'frontend') $entry.path
    if (-not (Test-Path -LiteralPath $packageDir)) { continue }
    $name=[Regex]::Replace(($entry.path+'@'+$entry.version),'[^A-Za-z0-9_.@-]','_')
    $files=Get-ChildItem -LiteralPath $packageDir -File | Where-Object { $_.Name -match '^(LICENSE|COPYING|NOTICE)' }
    foreach ($file in $files) { Copy-Item -LiteralPath $file.FullName -Destination (Join-Path $licenseRoot ($name+'--'+$file.Name)) -Force }
}
Copy-Item -LiteralPath (Join-Path $projectRoot 'THIRD_PARTY_NOTICES.md') -Destination (Join-Path $projectRoot 'bin') -Force
