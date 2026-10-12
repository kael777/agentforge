[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\..')).Path
$backendDir = Join-Path $projectRoot 'backend\gofiber-gorm-next'
$frontendDir = Join-Path $projectRoot 'frontend\web'

Push-Location $backendDir
try {
    & go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Backend tests failed.' }
} finally {
    Pop-Location
}

Push-Location $frontendDir
try {
    & npm run test
    if ($LASTEXITCODE -ne 0) { throw 'Frontend tests failed.' }
} finally {
    Pop-Location
}
