[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidatePattern('^v\d+\.\d+\.\d+$')]
    [string]$Version
)

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\..')).Path
Push-Location $projectRoot
try {
    $changes = & git status --porcelain
    if ($LASTEXITCODE -ne 0) { throw 'Unable to inspect Git status.' }
    if ($changes) { throw 'Git working tree must be clean before publishing.' }

    & (Join-Path $PSScriptRoot 'check.ps1')
    & git tag -a $Version -m "AgentForge $Version"
    if ($LASTEXITCODE -ne 0) { throw 'Unable to create Git tag.' }
    & git push origin $Version
    if ($LASTEXITCODE -ne 0) { throw 'Unable to push Git tag.' }
} finally {
    Pop-Location
}
