[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\..')).Path
$frontendDir = Join-Path $projectRoot 'frontend\web'

& (Join-Path $PSScriptRoot 'build.bat') all
if ($LASTEXITCODE -ne 0) { throw 'Backend build failed.' }

Push-Location $frontendDir
try {
    & npm run build
    if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' }
} finally {
    Pop-Location
}
