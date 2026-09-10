<#
.SYNOPSIS
    Prepara la infraestructura de SQL Server aislada en Docker, ejecuta seed y corre las pruebas de integración.
.DESCRIPTION
    NUNCA apunta a producción (Caproba01\vbadilla, CONTABILIDAD, C:\Backups).
    Utiliza exclusivamente el contenedor de desarrollo y la base CONTABILIDAD_TEST.
#>
param (
    [string]$HostName = "localhost",
    [int]$Port = 14333,
    [string]$Database = "CONTABILIDAD_TEST",
    [string]$User = "sa",
    [string]$Password = "TestPassw0rd!123",
    [int]$TimeoutSec = 60,
    [switch]$SkipDocker,
    [switch]$Cleanup
)

$ErrorActionPreference = "Stop"

Write-Host "=== FEMUCARIBE Backup Agent — Test Runner SQL Server Aislado ===" -ForegroundColor Cyan

# 1. Comprobar Docker si no se indicó SkipDocker
if (-not $SkipDocker) {
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        Write-Error "Docker no se encuentra instalado o no está en el PATH. Para ejecutar pruebas contra SQL Server aislado levantá Docker Desktop o especificá un SQL Server de laboratorio con -SkipDocker."
    }

    Write-Host "[1/5] Iniciando contenedor SQL Server en puerto $Port..." -ForegroundColor Yellow
    $composeFile = Join-Path $PSScriptRoot "..\docker\sqlserver\docker-compose.yml"
    $env:TEST_SQLSERVER_PORT = $Port
    docker compose -f $composeFile up -d

    Write-Host "[2/5] Esperando a que SQL Server esté listo (timeout: ${TimeoutSec}s)..." -ForegroundColor Yellow
    $startTime = Get-Date
    $ready = $false
    while (((Get-Date) - $startTime).TotalSeconds -lt $TimeoutSec) {
        try {
            $tcp = New-Object System.Net.Sockets.TcpClient
            $tcp.Connect($HostName, $Port)
            if ($tcp.Connected) {
                $tcp.Close()
                $ready = $true
                break
            }
        } catch {
            Start-Sleep -Seconds 2
        }
    }

    if (-not $ready) {
        Write-Error "Tiempo de espera agotado esperando que SQL Server responda en $HostName:$Port."
    }
    Write-Host "SQL Server está respondiendo en el puerto $Port." -ForegroundColor Green
}

# 2. Configurar variables de entorno para los tests
$env:TEST_DATABASE_DRIVER = "sqlserver"
$env:TEST_SQLSERVER_HOST = $HostName
$env:TEST_SQLSERVER_PORT = $Port
$env:TEST_SQLSERVER_DATABASE = $Database
$env:TEST_SQLSERVER_USER = $User
$env:TEST_SQLSERVER_PASSWORD = $Password
$env:TEST_BACKUP_ROOT = "C:\BackupsTest\"

# 3. Ejecutar suite de integración con tags
Write-Host "[3/5] Ejecutando pruebas de integración con -tags=sqlserver..." -ForegroundColor Yellow
$repoRoot = Join-Path $PSScriptRoot ".."
Push-Location $repoRoot
try {
    go test -v -tags=sqlserver ./...
    $testResult = $LASTEXITCODE
} finally {
    Pop-Location
}

# 4. Limpieza opcional
if ($Cleanup) {
    Write-Host "[4/5] Limpiando entorno de pruebas..." -ForegroundColor Yellow
    & "$PSScriptRoot\cleanup-test.ps1"
    if (-not $SkipDocker) {
        docker compose -f $composeFile down
    }
}

if ($testResult -eq 0) {
    Write-Host "=== TODAS LAS PRUEBAS DE INTEGRACIÓN PASARON CON ÉXITO ===" -ForegroundColor Green
} else {
    Write-Error "Las pruebas de integración fallaron con código de salida: $testResult"
}
