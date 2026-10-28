[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\..')).Path
$backendDir = Join-Path $projectRoot 'backend\api'
$frontendDir = Join-Path $projectRoot 'frontend\web'
$composeFile = Join-Path $projectRoot 'devops\docker\docker-compose.yml'

if (-not (Test-Path -LiteralPath (Join-Path $backendDir '.env'))) {
    Copy-Item -LiteralPath (Join-Path $backendDir '.env.example') -Destination (Join-Path $backendDir '.env')
}
if (-not (Test-Path -LiteralPath (Join-Path $frontendDir '.env'))) {
    Copy-Item -LiteralPath (Join-Path $frontendDir '.env.example') -Destination (Join-Path $frontendDir '.env')
}
if (-not (Test-Path -LiteralPath (Join-Path $frontendDir 'node_modules'))) {
    Push-Location $frontendDir
    try {
        & npm ci
        if ($LASTEXITCODE -ne 0) { throw 'npm ci failed.' }
    } finally {
        Pop-Location
    }
}

& docker compose -p agentforge -f $composeFile up -d --wait mysql
if ($LASTEXITCODE -ne 0) { throw 'MySQL failed to start.' }

Push-Location $backendDir
try {
    & go run ./cmd/migrate
    if ($LASTEXITCODE -ne 0) { throw 'Database migration failed.' }
} finally {
    Pop-Location
}

$backendJob = Start-Job -Name 'agentforge-backend' -ScriptBlock {
    Set-Location $using:backendDir
    & go run ./cmd/api
}
$frontendJob = Start-Job -Name 'agentforge-frontend' -ScriptBlock {
    Set-Location $using:frontendDir
    & npm run dev
}
$jobs = @($backendJob, $frontendJob)

Write-Host 'Backend: http://localhost:55555'
Write-Host 'Frontend: http://localhost:5173'
Write-Host 'Press Ctrl+C to stop application processes; run docker compose down to stop MySQL.'

try {
    while (($jobs | Where-Object State -EQ 'Running').Count -eq $jobs.Count) {
        Wait-Job -Job $jobs -Any -Timeout 1 | Out-Null
        $jobs | Receive-Job
    }
    $jobs | Receive-Job
    $failedJob = $jobs | Where-Object State -EQ 'Failed' | Select-Object -First 1
    if ($failedJob) { throw "$($failedJob.Name) failed." }
} finally {
    $jobs | Stop-Job -ErrorAction SilentlyContinue
    $jobs | Remove-Job -Force -ErrorAction SilentlyContinue
}
