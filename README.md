# FEMUCARIBE Backup Agent — Fase 1, 2, 2.1 & 2.2

Agente de backups para SQL Server `CONTABILIDAD` con arquitectura hexagonal limpia, interfaz gráfica de terminal moderna (**TUI** con Bubble Tea v2, Bubbles v2, Lip Gloss v2 y Glamour v2), subida a **Cloudflare R2** (S3 compatible), protección de credenciales con **Windows DPAPI** y soporte para ejecución desatendida vía Windows Task Scheduler.

- **Fase 1:** Backup local en `C:\Backups\`, verificación `RESTORE VERIFYONLY`, hash SHA-256 por streaming, lock file contra concurrencia y rotación local (3 copias).
- **Fase 2:** Subida a **Cloudflare R2** con verificación de integridad por tamaño, rotación remota a 1 copia, gestión segura de credenciales vía **Windows DPAPI** (`config.dat`) y tolerancia a fallos con `pending_sync`.
- **Fase 2.1:** Refactor transversal: CLI con **Cobra**, desacoplamiento total de la capa de aplicación (`internal/application/`), interfaz de almacenamiento (`storage.Backend`), clasificación de errores (`RetryableError`), logging estructurado con `log/slog` y códigos de salida centralizados.
- **Fase 2.2:** **Dashboard TUI completo**: Interfaz de terminal enriquecida con Bubble Tea v2, Bubbles v2 (viewport, textinput, spinner), Lip Gloss v2 y Glamour v2. Ejecución asíncrona sin bloquear el event loop, visor de logs con scroll, formulario de credenciales protegido y manual de ayuda integrado.

---

## Arquitectura

```text
femucaribe-backup-agent/
├── cmd/
│   └── backup-agent/
│       ├── main.go              # Punto de entrada ultra delgado (solo invoca cli.Execute())
│       └── main_test.go
├── internal/
│   ├── ui/                      # Capa de presentación TUI (Bubble Tea v2, Lip Gloss, Glamour)
│   │   ├── app.go               # Modelo raíz tea.Model y router de navegación
│   │   ├── dashboard.go         # Pantalla principal (estado de copia y grilla de backends)
│   │   ├── backup_progress.go   # Ejecución asíncrona de backup con spinner animado
│   │   ├── sync.go              # Sincronización asíncrona a Cloudflare R2
│   │   ├── status.go            # Vista detallada de parámetros del agente
│   │   ├── logs.go              # Visor interactivo de logs diarios con viewport scrollable
│   │   ├── configure.go         # Formulario de credenciales con textinput y password masking
│   │   ├── help.go              # Visor de ayuda Markdown con Glamour y viewport
│   │   ├── styles.go            # Paleta semántica y estilos centralizados de Lip Gloss v2
│   │   ├── content/             # Documentación Markdown embebida (embed.FS)
│   │   │   ├── about.md
│   │   │   ├── backup-help.md
│   │   │   ├── configuration-help.md
│   │   │   └── troubleshooting.md
│   │   └── ui_test.go
│   ├── cli/                     # Adaptadores Cobra y launcher TUI
│   │   ├── root.go              # Comando raíz, detección de TTY y mapeo de Exit Codes
│   │   ├── backup.go            # Subcomando 'agent backup'
│   │   ├── sync.go              # Subcomando 'agent sync'
│   │   ├── status.go            # Subcomando 'agent status'
│   │   ├── logs.go              # Subcomando 'agent logs'
│   │   ├── configure.go         # Subcomando 'agent configure'
│   │   ├── interactive.go       # Launcher de Bubble Tea (tea.NewProgram)
│   │   └── cli_test.go
│   ├── application/             # Casos de uso de negocio (100% desacoplados de Cobra, TUI y TTY)
│   │   ├── app.go               # Orquestador del ciclo de vida y constructor de dependencias
│   │   ├── backup.go            # Pipeline completo de backup
│   │   ├── sync.go              # Reintento de sincronizaciones pendientes
│   │   ├── status.go            # Consulta de estado consolidado
│   │   ├── logs.go              # Lectura de registros del día
│   │   ├── dto.go               # DTOs de presentación para la TUI
│   │   ├── errors.go            # Errores centinela de aplicación
│   │   └── app_test.go
│   ├── storage/                 # Abstracción de destinos de almacenamiento
│   │   ├── backend.go           # Interfaz Backend y RetryableError
│   │   ├── local/               # Backend de almacenamiento local en disco
│   │   └── r2/                  # Backend de Cloudflare R2 (S3 compatible)
│   ├── secrets/                 # Cifrado DPAPI (Windows) y gestión de config.dat
│   ├── logging/                 # Logging estructurado con log/slog y rotación diaria
│   ├── config/                  # Carga y validación de config.json
│   ├── state/                   # Persistencia atómica de estado en state.json
│   ├── lock/                    # Exclusión mutua (agent.lock con PID) y detección de huérfanos
│   ├── hasher/                  # Hash SHA-256 por streaming
│   ├── rotation/                # Algoritmo de retención y poda de backups locales
│   └── sqlbackup/               # Operaciones directas sobre SQL Server (go-mssqldb)
├── bin/                         # Binario compilado
├── config.example.json          # Plantilla de configuración
├── go.mod
├── go.sum
└── README.md
```

---

## Requisitos

- Go 1.25+ (probado con Go 1.27 en Windows; requerido por `go-mssqldb` v1.11).
- Windows en producción (ejecución bajo la identidad del servicio con acceso a SQL Server y a DPAPI).
- SQL Server accesible con Windows Integrated Authentication.

---

## Configuración

El agente busca junto al binario:

| Archivo       | Tipo / Formato | Descripción |
|---------------|----------------|-------------|
| `config.json` | JSON (texto)   | Parámetros locales opcionales: `backup_dir`, `server`, `database`, `retain` (default 3), timeouts. |
| `config.dat`  | Binario cifrado| Credenciales de R2 cifradas con Windows DPAPI: Endpoint, Bucket, Access Key, Secret Key. |
| `state.json`  | JSON (texto)   | Estado persistente: `last_run_date`, `last_backup_file`, `sha256`, `pending_sync.r2`, `r2_last_synced_file`. |
| `agent.lock`  | Texto con PID  | Lock file para evitar ejecuciones concurrentes y reclamar instancias muertas. |
| `logs/`       | Directorio     | Archivos de log rotativos diarios: `agent-YYYY-MM-DD.log`. |

---

## Uso del CLI

### Compilación

```powershell
go build -o bin/backup-agent.exe ./cmd/backup-agent
```

### Comportamiento del Comando Raíz

El binario puede ejecutarse directamente como `backup-agent.exe`:
- **En entorno interactivo (con TTY):** Inicia automáticamente el **Dashboard TUI**.
- **En entorno desatendido (sin TTY / Task Scheduler):** Muestra el mensaje de ayuda (`--help`) y finaliza con código de salida `1` (`ExitGeneralErr`), **sin bloquear esperando entrada por stdin**.

---

## Interfaz de Terminal (TUI Dashboard)

Al ejecutar `backup-agent.exe` en una consola o terminal interactiva (o mediante `backup-agent interactive`), se abre el dashboard visual:

```text
╭────────────────────────────────────────────────────────────╮
│ FEMUCARIBE BACKUP AGENT                         v2.2       │
├────────────────────────────────────────────────────────────┤
│  ESTADO DEL BACKUP                                         │
│  Última copia:    2026-09-10 12:00:00                      │
│  Archivo:         CONTABILIDAD_20260910_1200.bak           │
│  SHA-256:         5e884898da28047151d0...                  │
│  Resultado:       ● EXITOSO                                │
│                                                            │
│  DESTINOS DE ALMACENAMIENTO                                │
│  ● Local          OK                                       │
│  ● R2             OK / PENDING / ERROR                     │
│  ○ Server         Not configured                           │
├────────────────────────────────────────────────────────────┤
│  [B] Backup  [S] Estado  [L] Logs  [C] Config  [Y] Sync    │
│  [H] Ayuda   [Q] Salir                                     │
╰────────────────────────────────────────────────────────────╯
```

### Navegación y Pantallas de la TUI

| Tecla | Pantalla / Acción | Descripción |
|:-----:|-------------------|-------------|
| **`B`** | **Ejecución de Backup** | Dispara el pipeline completo en segundo plano (asíncrono vía `tea.Cmd`) mostrando un spinner animado `Dot` y el estado por etapa (SQL Server, Local, R2) sin congelar la interfaz. |
| **`S`** | **Estado Detallado** | Consulta los parámetros de base de datos, ruta de retención, lock file y estado remoto de R2. |
| **`L`** | **Visor de Logs** | Despliega las últimas 100 líneas del log del día en un `viewport` interactivo con soporte de scroll (flechas, `PgUp`/`PgDn` y rueda del mouse) y recarga con `[R]`. |
| **`C`** | **Configuración R2** | Formulario interactivo con `textinput` para Endpoint, Bucket, Access Key y Secret Key (con máscara de contraseña `EchoPassword`). Cifra y guarda de forma segura con Windows DPAPI en `config.dat`. |
| **`Y`** | **Sincronización R2** | Reintenta en segundo plano cualquier subida pendiente a Cloudflare R2 con spinner activo. |
| **`H`** | **Manual y Ayuda** | Visor de documentación Markdown renderizada con **Glamour** en modo oscuro. Permite alternar entre temas presionando `[1]` (Acerca de), `[2]` (Ciclo de Backup), `[3]` (Configuración) y `[4]` (Resolución de Problemas). |
| **`Esc`** | **Volver** | Regresa inmediatamente al Dashboard principal desde cualquier pantalla. |
| **`Q`** | **Salir** | Cierra la aplicación de forma limpia. |

---

## Subcomandos para Automatización y Scripts

#### 1. Ejecutar Backup (`backup`)

```powershell
# Modo normal: respeta la idempotencia diaria (si ya corrió hoy, sale con código 0)
.\bin\backup-agent.exe backup

# Modo desatendido para Task Scheduler (garantiza cero prompts o lectura de stdin)
.\bin\backup-agent.exe backup --unattended

# Forzar ejecución manual (ignora si ya corrió hoy)
.\bin\backup-agent.exe backup --force

# Usar archivo de configuración alternativo
.\bin\backup-agent.exe backup --config C:\soporte\config.custom.json --force
```

#### 2. Sincronización Remota de Pendientes (`sync`)

```powershell
# Sube a R2 el backup pendiente registrado en state.json si hubo fallos de red previos
.\bin\backup-agent.exe sync

# Modo desatendido
.\bin\backup-agent.exe sync --unattended

# Forzar subida del último backup registrado aunque no esté marcado como pendiente
.\bin\backup-agent.exe sync --force
```

#### 3. Consultar Estado (`status`)

```powershell
.\bin\backup-agent.exe status
```

#### 4. Consultar Logs Recientes (`logs`)

```powershell
# Muestra las últimas 20 líneas del log del día (default)
.\bin\backup-agent.exe logs

# Especificar cantidad de líneas
.\bin\backup-agent.exe logs -n 50
```

#### 5. Configuración por Consola (`configure`)

```powershell
.\bin\backup-agent.exe configure
```

---

## Códigos de Salida Centralizados (Exit Codes)

| Código | Constante         | Significado |
|:------:|-------------------|-------------|
| **`0`** | `ExitOK`          | Operación completada con éxito o backup ya realizado el día de hoy (idempotencia). |
| **`1`** | `ExitGeneralErr`  | Error general de ejecución (fallo en SQL, error de I/O, sin TTY en comando raíz). |
| **`2`** | `ExitConfigErr`   | Configuración inválida, faltante o sintaxis corrupta (`config.json`). |
| **`3`** | `ExitPendingSync` | Backup local creado y verificado con éxito, pero la subida a R2 falló (quedó en `pending_sync`). |
| **`4`** | `ExitLocked`      | Otra instancia del agente se encuentra en ejecución (`agent.lock` activo con PID vivo). |

---

## Configuración en Windows Task Scheduler

Para la ejecución desatendida en producción:

1. **Acción:** `Iniciar un programa`
   - **Programa o script:** `C:\Agente\backup-agent.exe`
   - **Agregar argumentos:** `backup --unattended`
   - **Iniciar en:** `C:\Agente\`
2. **Disparadores (Triggers):**
   - Disparador programado diario (ej: `23:00`).
   - Disparador al iniciar el sistema (*At startup*) con retraso de 5 a 10 minutos para asegurar que SQL Server esté en línea.
3. **Seguridad:**
   - Cuenta de servicio con permisos en SQL Server y DPAPI.

### Monitoreo de Logs en Vivo

```powershell
Get-Content C:\Agente\logs\agent-2026-09-10.log -Wait
```

---

---

## Testing e Infraestructura de Pruebas (Fase 2.3)

> [!CAUTION]
> ### ⚠️ TESTING SAFETY
>
> Los tests **NUNCA** deben ejecutarse contra recursos de producción:
> - **Instancia:** `Caproba01\vbadilla`
> - **Base de datos:** `CONTABILIDAD`
> - **Directorio:** `C:\Backups\`
>
> La infraestructura de testing cuenta con un guardián programático estricto ([`testenv.ValidateSafety`](file:///c:/DEV/femucaribe-backup-agent/test/testenv/safety.go)) que valida antes de cada prueba que ni el host, ni la base, ni las rutas apunten a producción. Si detecta alguno de estos valores en modo test, **aborta la ejecución inmediatamente**.

### Estrategia de Testing y Motores

El sistema separa estrictamente dos niveles de pruebas de integración:

1. **Suite Rápida Aislada (SQLite)**:
   - Utiliza `modernc.org/sqlite` (Go puro, sin requerir CGO/GCC en Windows).
   - Valida el ciclo de vida completo de la aplicación en milisegundos: creación de base, aplicación del seed determinístico, generación de `.bak`, verificación de hash SHA-256, consistencia de `state.json`, rotación local, resiliencia ante `kill` mediante failpoints (`TEST_FAILPOINT=after_backup_started`), y restauración con comparación de snapshots determinísticos (`AssertDatabaseEquivalent`).
   - Se ejecuta con el comando estándar sin requerir Docker ni servicios externos.

2. **Integración con SQL Server Aislado (Docker / Tag `sqlserver`)**:
   - Levanta una instancia de SQL Server 2022 en un contenedor Docker en el puerto aislado `14333` con credenciales de prueba.
   - Valida específicamente comandos nativos de SQL Server: `BACKUP DATABASE ... WITH COMPRESSION, CHECKSUM`, `RESTORE VERIFYONLY`, lectura de layout físico con `RESTORE FILELISTONLY`, y restauración a base temporal secundaria (`CONTABILIDAD_TEST_RESTORE`).

### Dataset Determinístico (Fixtures de Seed)

Los tests utilizan un dataset determinístico formal ubicado en `test/fixtures/seed/`:
- **`customers`**: 10 registros con claves primarias y caracteres especiales.
- **`accounts`**: 20 cuentas bancarias vinculadas.
- **`invoices`**: 50 facturas con montos, fechas y estados.
- **`transactions`**: 200 transacciones para verificar consistencia relacional e integridad total tras restauración.

### Comandos de Ejecución de Pruebas

#### 1. Suite Rápida Completa (Unitarios + Integración SQLite + Resiliencia)
No requiere Docker ni dependencias externas. Se completa en ~2 segundos:

```powershell
go test -count=1 ./...
```

#### 2. Tests con SQL Server Aislado en Docker
Requiere Docker Desktop en ejecución:

```powershell
# Levantar SQL Server de pruebas, esperar TCP y ejecutar la suite completa:
.\scripts\test-sqlserver.ps1

# O manualmente:
docker compose -f docker/sqlserver/docker-compose.yml up -d
go test -v -tags=sqlserver ./test/integration/sqlserver/...
docker compose -f docker/sqlserver/docker-compose.yml down
```

#### 3. Limpieza de Artefactos de Prueba
Elimina de forma segura carpetas temporales en `C:\BackupsTest\` protegiendo categóricamente `C:\Backups\`:

```powershell
.\scripts\cleanup-test.ps1
```

#### 4. Conservación de Artefactos para Diagnóstico
Si se desea inspeccionar los archivos generados tras una falla:

```powershell
$env:KEEP_TEST_ARTIFACTS="true"
go test ./...
```

