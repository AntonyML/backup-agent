<#
.SYNOPSIS
    Limpia de forma segura los artefactos y directorios de prueba.
.DESCRIPTION
    Posee protecciones estrictas para asegurar que NUNCA elimine C:\Backups de producción.
#>
param (
    [string]$TargetDir = "C:\BackupsTest",
    [switch]$StopDocker
)

$ErrorActionPreference = "Stop"

Write-Host "=== Limpieza de Entorno de Pruebas ===" -ForegroundColor Cyan

# 1. Validación de seguridad crítica
if ([string]::IsNullOrWhiteSpace($TargetDir)) {
    Write-Error "La ruta de destino no puede estar vacía."
}

$normalized = [System.IO.Path]::GetFullPath($TargetDir).TrimEnd('\', '/')
$forbidden = [System.IO.Path]::GetFullPath("C:\Backups").TrimEnd('\', '/')

if ($normalized -ieq $forbidden) {
    Write-Error "PELIGRO DE SEGURIDAD: Se intentó ejecutar limpieza sobre el directorio de PRODUCCIÓN 'C:\Backups'. Operación abortada de inmediato."
}

if (-not ($normalized -ilike "*BackupsTest*")) {
    Write-Error "La ruta de limpieza debe contener explícitamente 'BackupsTest' para prevenir borrados accidentales en otras ubicaciones. Ruta recibida: $TargetDir"
}

# 2. Eliminación segura del directorio de test
if (Test-Path $normalized) {
    Write-Host "Eliminando directorio de pruebas: $normalized" -ForegroundColor Yellow
    Remove-Item -Path $normalized -Recurse -Force
    Write-Host "Directorio de pruebas eliminado correctamente." -ForegroundColor Green
} else {
    Write-Host "El directorio $normalized no existe; no se requiere limpieza en disco." -ForegroundColor Gray
}

# 3. Detener contenedor Docker si se solicitó
if ($StopDocker) {
    if (Get-Command docker -ErrorAction SilentlyContinue) {
        $composeFile = Join-Path $PSScriptRoot "..\docker\sqlserver\docker-compose.yml"
        if (Test-Path $composeFile) {
            Write-Host "Deteniendo contenedor Docker de pruebas..." -ForegroundColor Yellow
            docker compose -f $composeFile down -v
            Write-Host "Contenedor de pruebas detenido y volumen liberado." -ForegroundColor Green
        }
    }
}

Write-Host "Limpieza completada exitosamente." -ForegroundColor Green
