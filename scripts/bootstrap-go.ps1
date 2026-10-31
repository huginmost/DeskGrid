param()
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$toolRoot = Join-Path $projectRoot '.tools'
New-Item -ItemType Directory -Path $toolRoot -Force | Out-Null
$archive = Join-Path $toolRoot 'go1.27.3.windows-amd64.zip'
Invoke-WebRequest -Uri 'https://go.dev/dl/go1.27.3.windows-amd64.zip' -OutFile $archive
$expected = 'F26081F438319757F8A1B46D7D0B2C5EC7583D4038E485E0AA725CFDE7885FFF'
if ((Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash -ne $expected) { throw 'Go 下载校验失败，未解压。' }
Expand-Archive -LiteralPath $archive -DestinationPath $toolRoot -Force
& (Join-Path $toolRoot 'go\bin\go.exe') version
if ($LASTEXITCODE -ne 0) { throw 'Go 工具链无法运行' }
