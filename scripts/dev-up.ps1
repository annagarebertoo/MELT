$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$envFile = Join-Path $root '.env.local'
$exampleFile = Join-Path $root '.env.local.example'

if (-not (Test-Path -LiteralPath $envFile)) {
    Copy-Item -LiteralPath $exampleFile -Destination $envFile
    Write-Host 'Se creó .env.local con credenciales ficticias solo para desarrollo local.'
}

Push-Location $root
try {
    docker compose --project-name melt-local --env-file .env.local -f compose.yaml -f compose.local.yaml up --build -d
    if ($LASTEXITCODE -ne 0) {
        throw 'Docker Compose no pudo iniciar MELT.'
    }
    docker compose --project-name melt-local --env-file .env.local -f compose.yaml -f compose.local.yaml ps
    if ($LASTEXITCODE -ne 0) {
        throw 'No se pudo consultar el estado de MELT.'
    }
    $gatewayPort = '8088'
    $gatewaySetting = Get-Content -LiteralPath $envFile | Where-Object { $_ -match '^MELT_GATEWAY_PORT=' } | Select-Object -First 1
    if ($gatewaySetting -match '^MELT_GATEWAY_PORT=(\d+)') {
        $gatewayPort = $Matches[1]
    }
    $healthUrl = "http://localhost:$gatewayPort/health"
    $healthy = $false
    for ($attempt = 0; $attempt -lt 30; $attempt++) {
        try {
            $response = Invoke-WebRequest -Uri $healthUrl -TimeoutSec 2
            if ($response.StatusCode -eq 200) {
                $healthy = $true
                break
            }
        } catch {
            Start-Sleep -Seconds 2
        }
    }
    if (-not $healthy) {
        throw "Gateway local no respondió en $healthUrl. Revisá el estado de Docker Compose."
    }
    Write-Host "MELT local: http://localhost:$gatewayPort"
    Write-Host 'Administrador local: admin@melt.local (contraseña en .env.local)'
}
finally {
    Pop-Location
}
