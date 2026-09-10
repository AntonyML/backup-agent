# FEMUCARIBE Backup Agent — Fase 1, 2 & 2.1

Agente de backups para SQL Server `CONTABILIDAD` con arquitectura desacoplada (Clean/Hexagonal Architecture), CLI moderno con Cobra, subida a **Cloudflare R2** (S3 compatible), protección de credenciales con **Windows DPAPI**, y preparación para backend de servidor en Fase 3.

- **Fase 1:** Backup local en `C:\Backups\`, verificación `RESTORE VERIFYONLY`, hash SHA-256 por streaming, lock file contra concurrencia y rotación local (3 copias).
- **Fase 2:** Subida a **Cloudflare R2** con verificación de integridad por tamaño, rotación remota a 1 copia, gestión segura de credenciales vía **Windows DPAPI** (`config.dat`) y tolerancia a fallos con `pending_sync`.
- **Fase 2.1:** Refactor transversal: CLI migrado a **Cobra** con adaptadores delgados, desacoplamiento total de la capa de aplicación (`internal/application/`) de la consola/TTY, interfaz abstracta de almacenamiento (`storage.Backend`), clasificación de errores (`RetryableError`), logging estructurado con `log/slog`, y códigos de salida centralizados.

---

## Arquitectura

```text
femucaribe-backup-agent/
├── cmd/
│   └── backup-agent/
│       ├── main.go              # Punto de entrada ultra delgado (solo invoca cli.Execute())
│       └── main_test.go
├── internal/
│   ├── cli/                     # Adaptadores Cobra y modo interactivo por consola
│   │   ├── root.go              # Comando raíz, detección de TTY y mapeo de Exit Codes
│   │   ├── backup.go            # Subcomando 'agent backup'
│   │   ├── sync.go              # Subcomando 'agent sync'
│   │   ├── status.go            # Subcomando 'agent status'
│   │   ├── logs.go              # Subcomando 'agent logs'
│   │   ├── configure.go         # Subcomando 'agent configure'
│   │   ├── interactive.go       # Menú interactivo por TTY
│   │   └── cli_test.go
│   ├── application/             # Casos de uso de negocio (100% desacoplados de Cobra y stdin)
│   │   ├── app.go               # Orquestador del ciclo de vida y constructor de dependencias
│   │   ├── backup.go            # Pipeline completo de backup
│   │   ├── sync.go              # Reintento de sincronizaciones pendientes
│   │   ├── status.go            # Consulta de estado consolidado
│   │   ├── logs.go              # Lectura de registros del día
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
| `config.dat`  | Binario cifrado| Credenciales de R2 cifradas con Windows DPAPI vía `backup-agent configure`: Endpoint, Bucket, Access Key, Secret Key. |
| `state.json`  | JSON (texto)   | Estado persistente: `last_run_date`, `last_backup_file`, `sha256`, `pending_sync.r2`, `r2_last_synced_file`. |
| `agent.lock`  | Texto con PID  | Lock file para evitar ejecuciones concurrentes y reclamar instancias muertas. |
| `logs/`       | Directorio     | Archivos de log rotativos diarios: `agent-YYYY-MM-DD.log`. |

### Configuración inicial de Cloudflare R2 (DPAPI)

Para registrar o actualizar las credenciales de R2 de forma segura (sin texto plano en disco ni variables de entorno):

```powershell
.\bin\backup-agent.exe configure
```

El asistente interactivo solicitará:
1. **Endpoint de R2**: ej: `https://<account_id>.r2.cloudflarestorage.com`
2. **Nombre del Bucket**: ej: `femucaribe-backups`
3. **R2 Access Key ID**
4. **R2 Secret Access Key**

Los valores se cifran usando `CryptProtectData` (DPAPI de Windows) con scope `CURRENT_USER` y se almacenan en `config.dat`. Solo se descifran en memoria RAM en tiempo de ejecución.

---

## Uso del CLI

### Compilación

```powershell
go build -o bin/backup-agent.exe ./cmd/backup-agent
```

### Comportamiento del Comando Raíz

El binario puede ejecutarse directamente como `backup-agent.exe`:
- **En entorno interactivo (con TTY):** Inicia automáticamente el menú interactivo por consola.
- **En entorno desatendido (sin TTY / Task Scheduler):** Muestra el mensaje de ayuda (`--help`) y finaliza con código de salida `1` (`ExitGeneralErr`), **sin bloquear esperando entrada por stdin**.

Para automatizaciones o tareas programadas, los comandos deben ser **siempre explícitos**.

### Subcomandos Disponibles

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

Muestra en formato tabular o de clave-valor:
- Última fecha de ejecución.
- Ruta del último archivo de backup generado y su hash SHA-256.
- Estado de sincronización pendiente (`pending_sync.r2`).
- Último archivo confirmado en R2.

#### 4. Consultar Logs Recientes (`logs`)

```powershell
# Muestra las últimas 20 líneas del log del día (default)
.\bin\backup-agent.exe logs

# Especificar cantidad de líneas
.\bin\backup-agent.exe logs -n 50
```

#### 5. Configuración de Credenciales (`configure`)

```powershell
.\bin\backup-agent.exe configure
```

#### 6. Menú Interactivo (`interactive`)

```powershell
.\bin\backup-agent.exe interactive
```

Despliega el menú de operaciones en pantalla:
```text
=== FEMUCARIBE Backup Agent ===
1. Configurar credenciales R2 (DPAPI)
2. Ejecutar backup ahora
3. Ver estado del agente
4. Ver logs recientes
5. Forzar sincronización pendiente
6. Salir
```

---

## Códigos de Salida Centralizados (Exit Codes)

El agente define una convención estricta y predecible de códigos de salida para su integración con sistemas de monitoreo y Windows Task Scheduler:

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
   - Disparador al iniciar el sistema (*At startup*) con un retraso de 5 a 10 minutos para asegurar que SQL Server esté en línea.
3. **Condiciones y Seguridad:**
   - Marcar *"Ejecutar tanto si el usuario inició sesión como si no"*.
   - Marcar *"Ejecutar con los privilegios más altos"*.
   - Usar la cuenta de servicio de Windows que posee permisos sobre la base de datos SQL y bajo la cual se ejecutó `backup-agent configure` (para acceso a DPAPI).

### Monitoreo de Logs en Vivo

Los logs se escriben mediante `log/slog` con flush inmediato a archivos diarios en formato estándar estructurado:

```powershell
Get-Content C:\Agente\logs\agent-2026-09-10.log -Wait
```

---

## Verificación y Tests

El proyecto cuenta con cobertura de pruebas unitarias sobre todas las capas del sistema sin dependencias externas:

```powershell
# Ejecutar análisis estático
go vet ./...

# Ejecutar suite de pruebas completa sin caché
go test -count=1 -v ./...
```

Cubre:
- Comandos Cobra, parsing de flags y mapeo de códigos de salida (`internal/cli`).
- Comportamiento del comando raíz con y sin TTY.
- Casos de uso de la capa de aplicación (`Backup`, `Sync`, `Status`, `TailLogs`) con mocks de motor SQL y backends.
- Aislamiento de la interfaz `Backend` y clasificación con `RetryableError`.
- Descarte de archivos temporales huérfanos (`.bak.tmp`) tras caídas abruptas.
- Reemplazo atómico de archivos.
- Cifrado DPAPI y persistencia segura de credenciales.
- Idempotencia diaria y gestión de `pending_sync`.
