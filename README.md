# FEMUCARIBE Backup Agent — Fase 1 & 2

Agente batch (vida corta, disparado por Windows Task Scheduler) para backup
de la base SQL Server `CONTABILIDAD`.
- **Fase 1:** Backup local en `C:\Backups\`, verificación `RESTORE VERIFYONLY`, hash SHA-256, lock file y rotación local (3 copias).
- **Fase 2:** Subida a **Cloudflare R2** (S3 compatible), secretos cifrados con **Windows DPAPI** (`config.dat`), rotación remota a 1 copia y tolerancia a fallos con `pending_sync`.

Sin copia a servidor ni reporte a Supabase (fases posteriores).

## Requisitos

- Go 1.25+ (probado con 1.27 en Windows; lo exige `go-mssqldb` v1.11)
- Windows en producción (corre con la identidad que tiene acceso a SQL y a DPAPI)
- SQL Server accesible con Windows Integrated Auth

## Configuración

El agente busca junto al binario:

| Archivo       | Descripción                                                                          |
|---------------|--------------------------------------------------------------------------------------|
| `config.json` | Opcional. Parámetros locales: `backup_dir`, `server`, `database`, `retain` (default 3). |
| `config.dat`  | Cifrado con DPAPI vía `backup-agent configure`: Endpoint, Bucket, Access Key y Secret Key. |
| `state.json`  | Estado persistente: `last_run_date`, `last_backup_file`, `sha256`, `pending_sync.r2`, `r2_last_synced_file`. |
| `agent.lock`  | Lock file con PID para evitar ejecuciones concurrentes.                              |
| `logs/`       | `agent-YYYY-MM-DD.log` rotativo por día (soporta `Get-Content -Wait`).                |

### Configuración inicial de Cloudflare R2 (DPAPI)

Para registrar las credenciales de R2 de manera segura (sin texto plano en disco ni variables de entorno):

```powershell
.\bin\backup-agent.exe configure
```

El asistente solicitará interactivamente:
1. **Endpoint de R2**: ej: `https://<account_id>.r2.cloudflarestorage.com`
2. **Nombre del Bucket**: ej: `femucaribe-backups`
3. **R2 Access Key ID**
4. **R2 Secret Access Key**

Los valores se cifran usando `CryptProtectData` (DPAPI de Windows) con scope `CURRENT_USER` y se guardan en `config.dat`. En tiempo de ejecución solo se descifran en memoria RAM.

### Configuración local (opcional)

Copiar y ajustar desde el ejemplo:

```powershell
Copy-Item config.example.json (Join-Path (Split-Path (Get-Command .\bin\backup-agent.exe).Source) config.json)
```

`config.example.json`:

```json
{
  "backup_dir": "C:\\Backups\\",
  "server": "Caproba01\\vbadilla",
  "database": "CONTABILIDAD",
  "retain": 3,
  "login_timeout_sec": 15,
  "backup_timeout_sec": 3600
}
```

> **Importante:** `backup_dir` debe ser una ruta **local al servidor SQL**.
> `BACKUP DATABASE` escribe desde el servicio SQL Server, no desde este
> proceso. La cuenta de servicio SQL necesita permiso de escritura ahí.

## Uso

```powershell
# Compilar binario
go build -o bin/backup-agent.exe ./cmd/backup-agent

# Configurar credenciales R2
.\bin\backup-agent.exe configure

# Modo normal (Task Scheduler): si ya hay backup de hoy, no repite
.\bin\backup-agent.exe

# Forzar corrida manual (soporte)
.\bin\backup-agent.exe --force

# Config alternativa
.\bin\backup-agent.exe --config C:\soporte\config.pruebas.json --force
```

Exit codes: `0` ok o ya-hecho-hoy, `1` error, `2` otra instancia en curso.

### Flujo de ejecución

1. **Lock file**: Adquiere `agent.lock`. Si hay un proceso huérfano con PID muerto, lo reclama.
2. **Limpieza de huérfanos**: Elimina `.bak.tmp` residuales de corridas previas interrumpidas.
3. **Sincronización pendiente (`pending_sync.r2`)**: Si hubo un fallo de red previo y el backup local existe, intenta subirlo a R2 **antes** de generar el nuevo backup del día.
4. **Idempotencia diaria**: Si ya se corrió hoy y no se indicó `--force`, finaliza con log informativo.
5. **Chequeo de espacio en disco**: Consulta tamaño estimado de la BD en SQL Server y valida espacio disponible en `backup_dir`.
6. **Backup y Verificación**:
   - `BACKUP DATABASE` a archivo temporal `.bak.tmp`.
   - `RESTORE VERIFYONLY` sobre el temporal.
   - Cálculo de hash SHA-256.
   - Rename atómico al nombre final: `CONTABILIDAD_YYYYMMDD_HHMM.bak`.
   - Registro en `state.json` y rotación local (mantiene las 3 copias más recientes).
7. **Subida y Rotación en Cloudflare R2**:
   - Subida con timeout de 10 min a la key `CONTABILIDAD/<nombre>.bak`.
   - Confirmación por verificación estricta de tamaño contra `HeadObject`.
   - Rotación remota: elimina copias viejas bajo `CONTABILIDAD/`, conservando únicamente la recién confirmada.
   - Si la subida falla por timeout o pérdida de conectividad, se marca `pending_sync.r2 = true` en `state.json` sin abortar el proceso (el backup local permanece íntegro y protegido).

## Task Scheduler (producción)

- Acción: `C:\Agente\backup-agent.exe` (sin argumentos), iniciar en `C:\Agente\`.
- Trigger diario (ej: 23:00) + trigger AtStartup con delay.
- Ejecutar como la cuenta de servicio configurada con DPAPI y acceso a SQL.

Ver logs en vivo estilo `tail -f`:

```powershell
Get-Content C:\Agente\logs\agent-2026-09-10.log -Wait
```

## Tests

### Unitarios (sin dependencias externas)

```powershell
go vet ./...
go test ./... -v
```

Cubre:
- DPAPI round-trip, carga y guardado seguro de `config.dat`.
- Compatibilidad hacia atrás de `state.json` y flags `pending_sync`.
- Mocks de cliente S3/R2 (subida, verificación de discrepancia de tamaño, rotación remota a 1 copia).
- Rotación local, detección de lock huérfano y hash SHA-256 por streaming.

### Integración con MinIO local (simulando R2 en Docker)

Para ejecutar pruebas contra un endpoint S3 local:

1. Levantar MinIO en Docker:
   ```powershell
   docker run -d -p 9000:9000 -p 9001:9001 --name minio `
     -e "MINIO_ROOT_USER=minioadmin" `
     -e "MINIO_ROOT_PASSWORD=minioadmin" `
     quay.io/minio/minio server /data --console-address ":9001"
   ```
2. Crear un bucket `femucaribe-backups` desde la consola web `http://localhost:9001`.
3. Configurar el agente para apuntar al MinIO local:
   - Endpoint: `http://localhost:9000`
   - Bucket: `femucaribe-backups`
   - Access Key: `minioadmin`
   - Secret Key: `minioadmin`

## Estructura

```text
femucaribe-backup-agent/
├── cmd/backup-agent/main.go       # flujo batch + subcomando configure + flags
├── internal/
│   ├── config/         # config.json con defaults de producción
│   ├── hasher/         # SHA-256 por stream
│   ├── lock/           # lock file con PID + detección de huérfanos
│   ├── logger/         # logs/agent-YYYY-MM-DD.log con Warnf/Errorf/Infof
│   ├── rotation/       # rotación local: conserva N .bak recientes
│   ├── secrets/        # cifrado DPAPI (Windows) y manejo de config.dat
│   ├── sqlbackup/      # BACKUP DATABASE / RESTORE VERIFYONLY (go-mssqldb)
│   ├── state/          # state.json: fechas, hashes y pending_sync
│   └── storage/r2/     # cliente Cloudflare R2, verificación de tamaño y rotación remota
├── config.example.json
├── go.mod
├── go.sum
└── README.md
```
