# FEMUCARIBE Backup Agent — Fase 1

Agente batch (vida corta, disparado por Windows Task Scheduler) para backup
de la base SQL Server `CONTABILIDAD`. **Fase 1: backup local + rotación.**
Sin R2, sin copia a servidor, sin reporte a Supabase (fases posteriores).

## Requisitos

- Go 1.25+ (probado con 1.27 en Windows; lo exige `go-mssqldb` v1.11)
- Windows en producción (corre con la identidad que tiene acceso al SQL)
- SQL Server accesible con Windows Integrated Auth

## Configuración

El agente busca junto al binario:

| Archivo       | Descripción                                                        |
|---------------|--------------------------------------------------------------------|
| `config.json` | Opcional. Si no existe, usa los defaults de producción.           |
| `state.json`  | Lo crea el agente: `last_run_date`, `last_backup_file`, `sha256`. |
| `agent.lock`  | Lock file con PID. Lo crea/libera el agente.                      |
| `logs/`       | `agent-YYYY-MM-DD.log` rotativo por día.                          |

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
> proceso. Si el agente corre en el mismo host que el SQL, `C:\Backups\`
> funciona. La cuenta de servicio SQL necesita permiso de escritura ahí.

## Uso

```powershell
go build -o bin/backup-agent.exe ./cmd/backup-agent

# Modo normal (Task Scheduler): si ya hay backup de hoy, no repite
.\bin\backup-agent.exe

# Forzar corrida manual (soporte)
.\bin\backup-agent.exe --force

# Config alternativa
.\bin\backup-agent.exe --config C:\soporte\config.pruebas.json --force
```

Exit codes: `0` ok o ya-hecho-hoy, `1` error, `2` otra instancia en curso.

Flujo de cada corrida: lock → crear `backup_dir` → descartar `.tmp`
huérfanos → idempotencia diaria → `BACKUP DATABASE` a `.bak.tmp` →
`RESTORE VERIFYONLY` → SHA-256 → rename atómico a
`CONTABILIDAD_YYYYMMDD_HHMM.bak` → guardar `state.json` → rotación (3).

## Task Scheduler (producción)

- Acción: `C:\Agente\backup-agent.exe` (sin argumentos), iniciar en `C:\Agente\`.
- Trigger diario (ej: 23:00) + trigger AtStartup con delay.
- Ejecutar como cuenta de servicio con acceso al SQL y a `C:\Backups\`.
- Ante un fallo, el detalle está en `logs\agent-YYYY-MM-DD.log`.

Ver logs en vivo estilo `tail -f`:

```powershell
Get-Content C:\Agente\logs\agent-2026-09-09.log -Wait
```

## Tests

Unitarios (sin SQL Server):

```powershell
go vet ./...
go test ./... -v
```

Integración (requiere SQL Server real o contenedor, **nunca** `CONTABILIDAD`):

```powershell
$env:FEMU_TEST_SQLSERVER = "localhost\SQLEXPRESS"
go test -tags integration ./internal/sqlbackup/ -v
```

Crea y borra la base `femucaribe_baktest` (BACKUP + VERIFY reales).

Recuperación ante kill (manual): matar el proceso a mitad del `BACKUP`,
verificar que queda un `.bak.tmp` huérfano y que la siguiente corrida lo
descarta y completa un backup válido sin duplicar ni corromper.
La lógica de descarte está cubierta además por `TestRemoveTmpOrphans_KillRecovery`.

## Estructura

```text
femucaribe-backup-agent/
├── cmd/backup-agent/main.go   # flujo batch + CLI (--force, --config)
├── internal/
│   ├── config/     # config.json con defaults de producción
│   ├── state/      # state.json: last_run_date, last_backup_file, sha256
│   ├── lock/       # lock file con PID + detección de huérfano (tasklist/signal 0)
│   ├── sqlbackup/  # BACKUP DATABASE / RESTORE VERIFYONLY (go-mssqldb, Win Auth)
│   ├── hasher/     # SHA-256 por stream
│   ├── rotation/   # conserva N .bak recientes, ignora .tmp
│   └── logger/     # logs/agent-YYYY-MM-DD.log (apto para Get-Content -Wait)
├── config.example.json
├── go.mod
└── README.md
```

## Decisiones (según prompt Fase 1)

- Driver `github.com/microsoft/go-mssqldb` (errores tipados, timeouts vía
  `context`); Windows Integrated Auth, sin credenciales en texto plano.
- Rotación ordena por nombre (`CONTABILIDAD_YYYYMMDD_HHMM.bak` es
  lexicográficamente cronológico); ignora `.tmp`, `state.json` y demás.
- `state.json` ausente = "nunca se corrió"; corrupto = se loguea y se trata
  como "nunca se corrió", nunca crashea. `Save` atómico (temporal + rename).
- Lock corrupto o con PID muerto se reclama; con PID vivo → exit 2.
- Chequeo de espacio libre **antes** del `BACKUP` (falla rápido).
- SQL detenido/instancia inaccesible → falla rápido, sin tocar `state.json`
  ni dejar archivos a medio escribir (el `.tmp` se borra; si es kill -9,
  lo descarta la siguiente corrida).
