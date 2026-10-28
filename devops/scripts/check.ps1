[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\..')).Path
$backendDir = Join-Path $projectRoot 'backend\api'
$frontendDir = Join-Path $projectRoot 'frontend\web'

Push-Location $backendDir
try {
    & go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'go vet failed.' }
} finally {
    Pop-Location
}

Push-Location $frontendDir
try {
    & npm run lint
    if ($LASTEXITCODE -ne 0) { throw 'Frontend lint failed.' }
} finally {
    Pop-Location
}

& (Join-Path $PSScriptRoot 'test.ps1')
& (Join-Path $PSScriptRoot 'build.ps1')
