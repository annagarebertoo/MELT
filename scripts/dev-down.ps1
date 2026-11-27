$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$envFile = Join-Path $root '.env.local'

if (-not (Test-Path -LiteralPath $envFile)) {
    Write-Host 'No existe .env.local; no hay un entorno local configurado para detener.'
    exit 0
}

Push-Location $root
try {
    docker compose --project-name melt-local --env-file .env.local -f compose.yaml -f compose.local.yaml down
    if ($LASTEXITCODE -ne 0) {
        throw 'Docker Compose no pudo detener MELT.'
    }
}
finally {
    Pop-Location
}
