# Backup Agent Enterprise

Agente de backups para SQL Server con arquitectura hexagonal limpia, interfaz gráfica de terminal moderna (**TUI** con Bubble Tea v2, Bubbles v2, Lip Gloss v2 y Glamour v2), subida a **Cloudflare R2** (S3 compatible), copia segura a **Servidor Remoto Windows / UNC** con rotación configurable, observabilidad centralizada en **Supabase** (PostgREST API), protección de credenciales con **Windows DPAPI** y soporte para ejecución desatendida vía Windows Task Scheduler.

> 📖 **Manual de Usuario y Operación:** Para consultar la guía de uso de la TUI paso a paso, atajos de teclado, reglas de negocio (idempotencia, rotación, permisos) y resolución de dudas frecuentes, consulte [GUIA_USUARIO.txt](GUIA_USUARIO.txt).

- **Fase 1:** Backup local en `C:\Backups\`, verificación `RESTORE VERIFYONLY`, hash SHA-256 por streaming, lock file contra concurrencia y rotación local (3 copias).
- **Fase 2:** Subida a **Cloudflare R2** con verificación de integridad por tamaño, rotación remota a 1 copia, gestión segura de credenciales vía **Windows DPAPI** (`config.dat`) y tolerancia a fallos con `pending_sync.r2`.
- **Fase 2.1:** Refactor transversal: CLI con **Cobra**, desacoplamiento total de la capa de aplicación (`internal/application/`), interfaz de almacenamiento (`storage.Backend`), clasificación de errores (`RetryableError`), logging estructurado con `log/slog` y códigos de salida centralizados.
- **Fase 2.2:** **Dashboard TUI completo**: Interfaz de terminal enriquecida con Bubble Tea v2, Bubbles v2 (viewport, textinput, spinner), Lip Gloss v2 y Glamour v2. Ejecución asíncrona sin bloquear el event loop, visor de logs con scroll, formulario de credenciales protegido y manual de ayuda integrado.
- **Fase 3:** **Copia a Servidor Remoto / Recurso Compartido (UNC)**: Transferencia segura mediante archivo temporal `.bak.tmp`, verificación estricta de integridad (doble validación de tamaño idéntico y SHA-256 por streaming), renombrado atómico, rotación de 10 copias más recientes, tolerancia a fallos con `pending_sync.server` y no destrucción de backups previos.
- **Fase 4:** **Registro Centralizado de Eventos en Supabase**: Observabilidad operacional centralizada mediante PostgREST API sobre HTTPS, buffering resiliente de eventos en `state.json` (`pending_events`), garantía de cero impacto en backups locales o remotos ante fallos de red, idempotencia estricta por `event_id`, y gestión de esquemas e infraestructura mediante el CLI oficial de Supabase.

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
│   │   ├── root.go              # Comando raíz, flags persistentes (--config, --profile) y Exit Codes
│   │   ├── backup.go            # Subcomando 'backup' (--unattended, --force, --profile)
│   │   ├── sync.go              # Subcomando 'sync' (reintento de R2, UNC y Supabase)
│   │   ├── status.go            # Subcomando 'status'
│   │   ├── logs.go              # Subcomando 'logs'
│   │   ├── doctor.go            # Subcomando 'doctor' (diagnóstico no destructivo de salud)
│   │   ├── profile.go           # Subcomando 'profile' (list, show, use)
│   │   ├── schedule.go          # Subcomando 'schedule' (install, update, remove, status con degradación D7)
│   │   ├── config.go            # Subcomando 'config validate'
│   │   ├── configure.go         # Subcomando 'configure'
│   │   ├── interactive.go       # Launcher de Bubble Tea (tea.NewProgram)
│   │   └── cli_test.go
│   ├── scheduler/               # Integración con Windows Task Scheduler (schtasks.exe)
│   │   ├── manager.go           # Operaciones schtasks (Install, Update, Delete, Status)
│   │   ├── spec.go              # Mapeo de triggers (daily, weekly, interval) y comando de elevación D7
│   │   ├── task_xml.go          # Generación de XML de tarea con ExecutionTimeLimit e IgnoreNew
│   │   └── status.go            # Consulta y parseo resiliente de estados en Windows (EN/ES)
│   ├── application/             # Casos de uso de negocio (100% desacoplados de Cobra, TUI y TTY)
│   │   ├── app.go               # Orquestador del ciclo de vida y buffering de eventos
│   │   ├── backup.go            # Pipeline completo de backup y emisión de eventos operacionales
│   │   ├── sync.go              # Reintento de sincronizaciones pendientes (R2, Server y Supabase)
│   │   ├── status.go            # Consulta de estado consolidado
│   │   ├── logs.go              # Lectura de registros del día
│   │   ├── dto.go               # DTOs de presentación para la TUI
│   │   ├── errors.go            # Errores centinela de aplicación
│   │   └── app_test.go
│   ├── events/                  # Modelo y repositorio de eventos para Supabase (Fase 4)
│   │   ├── event.go             # Entidad Event, constantes canónicas y sentinel errors
│   │   ├── supabase.go          # Cliente PostgREST con idempotencia y reintentos
│   │   └── event_test.go
│   ├── storage/                 # Abstracción de destinos de almacenamiento
│   │   ├── backend.go           # Interfaz Backend y RetryableError
│   │   ├── local/               # Backend de almacenamiento local en disco
│   │   ├── r2/                  # Backend de Cloudflare R2 (S3 compatible)
│   │   └── server/              # Backend para servidor remoto Windows / UNC (Fase 3)
│   ├── secrets/                 # Cifrado DPAPI (Windows) y gestión de config.dat
│   ├── logging/                 # Logging estructurado con log/slog y rotación diaria
│   ├── config/                  # Carga, validación y gestión de perfiles en config.json
│   ├── state/                   # Persistencia atómica de estado en state.json
│   ├── lock/                    # Exclusión mutua por perfil (agent-<perfil>.lock) y detección de huérfanos
│   ├── hasher/                  # Hash SHA-256 por streaming
│   ├── rotation/                # Algoritmo de retención y poda de backups locales
│   ├── sqlbackup/               # Operaciones directas sobre SQL Server (go-mssqldb)
│   └── version/                 # Información de versión del binario (v4.0.0)
│   ├── supabase/                # Configuración y migraciones oficiales de Supabase CLI
│   │   ├── config.toml          # Configuración del proyecto local/remoto
│   │   └── migrations/          # Migraciones SQL versionadas
│   ├── test/                    # Suite de testing aislada y suites de integración
│   │   ├── integration/         # Tests de integración (backup, recovery, rotation, server, supabase)
│   │   ├── testdb/              # Adaptadores SQLite y SQL Server
│   │   └── testenv/             # Guardián de seguridad anti-producción
│   ├── bin/                     # Binario compilado
│   ├── config.example.json      # Plantilla de configuración
│   ├── go.mod
│   ├── go.sum
│   └── README.md
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
| `config.json` | JSON (texto)   | Parámetros locales y remotos: `backup_dir`, `server`, `database`, `retain` (local, default 3), `remote_server` (`enabled`, `remote_path`, `keep` default 10, `timeout_sec`), `supabase` (`enabled`, `url`, `timeout_sec`). |
| `config.dat`  | Binario cifrado| Credenciales de R2 cifradas con Windows DPAPI: Endpoint, Bucket, Access Key, Secret Key. |
| `state.json`  | JSON (texto)   | Estado persistente: `last_run_date`, `last_backup_file`, `sha256`, `pending_sync` (`r2`, `server`), `r2_last_synced_file`, `server_last_synced_file`, `pending_events` (eventos de Supabase en buffer resiliente). |
| `agent-<perfil>.lock` | Texto con PID  | Lock file por perfil para evitar ejecuciones concurrentes del mismo perfil sin bloquear otros perfiles. |
| `logs/`       | Directorio     | Archivos de log rotativos diarios: `agent-YYYY-MM-DD.log`. |

### Variables de Entorno (Credenciales de Supabase)

Por principio de seguridad, las credenciales de Supabase **nunca** se almacenan en `config.json`. El agente las resuelve automáticamente a través del entorno:

| Variable | Propósito | Ejemplo |
|---|---|---|
| `SUPABASE_KEY` / `SUPABASE_API_KEY` | API Key pública (`anon`) o de servicio para autenticación PostgREST | `eyJhbGciOi...` |
| `SUPABASE_ACCESS_TOKEN` | Token de acceso personal para Supabase CLI / fallback de API | `sbp_...` |
| `SUPABASE_URL` | Sobrescritura opcional del endpoint HTTP | `https://oxpxyiucnzpedawwkosy.supabase.co` |

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

### Flag Persistente `--profile`

Todos los subcomandos aceptan el flag persistente `--profile <nombre>` (heredado del comando raíz):
- Si no se especifica `--profile`, el agente utiliza el perfil activo (`active_profile`) definido en `config.json` (por defecto `"full"`).
- Cada perfil cuenta con su propio archivo de exclusión mutua (`agent-<perfil>.lock`), permitiendo que ejecuciones de perfiles diferentes convivan sin bloquearse mutuamente.
- **Validación estricta (Exit Code 2)**: Si se especifica un perfil que no existe en `config.json`, el agente aborta de inmediato con código de salida **`2`** (`ExitConfigErr`), listando los perfiles disponibles.

```powershell
# Ejecutar backup bajo un perfil específico
.\bin\backup-agent.exe backup --profile diario-r2 --unattended

# Diagnosticar un perfil específico
.\bin\backup-agent.exe doctor --profile full

# Si el perfil no existe, devuelve exit code 2:
.\bin\backup-agent.exe backup --profile inexistente
# Error: error de configuración: el perfil "inexistente" no existe (disponibles: full, diario-r2)
# Exit Code: 2
```

---

#### 1. Gestión de Perfiles de Backup (`profile`)

Permite listar, inspeccionar y activar perfiles configurados en `config.json`. El destino local (`C:\Backups\`) está siempre implícito; cada perfil define qué plataformas remotas adicionales (`r2`, `server`) reciben el backup y cuál es su programación.

- **`profile list`**: Lista todos los perfiles configurados, destacando el activo con `*` y resumiendo tipo, plataformas y schedule:
  ```powershell
  .\bin\backup-agent.exe profile list
  ```
  *Salida de ejemplo:*
  ```text
  === Perfiles de backup ===
  * full             kind=custom   plataformas=r2, server                       schedule=diario a las 23:00
    ligero           kind=custom   plataformas=solo local                       schedule=cada 60 min

  Perfil activo: full
  ```

- **`profile show [nombre]`**: Muestra el detalle exhaustivo de un perfil (tipo, plataformas, schedule efectivo, herencia global, nombre de tarea de Windows y overrides de R2/UNC). Si se omite el argumento, muestra el perfil activo o el provisto vía `--profile`:
  ```powershell
  .\bin\backup-agent.exe profile show full
  ```
  *Salida de ejemplo:*
  ```text
  === Perfil full ===
  Tipo (kind):       custom
  Plataformas:       local, r2, server
  Schedule:          diario a las 23:00
  Hereda del global: true
  Tarea Windows:     FEMUCARIBE-Backup-full
  ```

- **`profile use <nombre>`**: Establece un perfil como el `active_profile` en `config.json` de forma atómica y persistente. Valida que el perfil exista (exit code 2 si no existe):
  ```powershell
  .\bin\backup-agent.exe profile use ligero
  # Perfil activo cambiado a "ligero" (guardado en C:\Agente\config.json).
  ```

---

#### 2. Diagnóstico de Salud y Conectividad (`doctor`)

Ejecuta una batería de comprobaciones **no destructivas** sobre el perfil especificado (o el activo) para validar la salud del entorno antes de un backup o tras cambios de red:

```powershell
# Diagnosticar el perfil activo
.\bin\backup-agent.exe doctor

# Diagnosticar un perfil específico
.\bin\backup-agent.exe doctor --profile full
```

*Verificaciones realizadas:*
1. **SQL Server**: Conectividad a la instancia, versión de SQL Server y existencia de la base de datos `CONTABILIDAD`.
2. **Directorio Local**: Existencia de `backup_dir` (`C:\Backups\`), permisos de lectura/escritura y espacio libre disponible en disco.
3. **Cloudflare R2**: Conectividad HTTP/S3, bucket accesible y permisos de escritura (si R2 está habilitado en el perfil).
4. **Servidor Remoto (UNC)**: Accesibilidad de la ruta de red `\\ServidorBackup\Backups\`, permisos de escritura y espacio (si UNC está habilitado en el perfil).
5. **Supabase**: Conectividad HTTPS a la API PostgREST y validación de API Key (si está habilitado).
6. **Tarea de Windows**: Estado de la tarea programada asociada al perfil (`Ready`, `Running`, `Disabled` o `no instalada`).

*Salida de ejemplo:*
```text
=== Doctor · perfil full ===

  ✔ SQL Server       Conectado a Caproba01\vbadilla (CONTABILIDAD)
  ✔ Almacén Local    C:\Backups\ (OK · 128 GB disponibles)
  ✔ Cloudflare R2    femucaribe-backups (OK)
  ✔ Servidor Remoto  \\ServidorBackup\Backups\CONTABILIDAD\ (OK)
  ✔ Supabase         https://oxpxyiucnzpedawwkosy.supabase.co (OK)
  ✔ Tarea Windows    FEMUCARIBE-Backup-full (Ready)

Todo en orden.
```

---

#### 3. Gestión de Tareas en Windows Task Scheduler (`schedule`) y Degradación D7

Administra de forma automatizada las tareas programadas de Windows para cada perfil utilizando el ejecutable oficial `schtasks.exe` y definiciones XML enriquecidas. Esto permite registrar configuraciones avanzadas que la interfaz estándar de `schtasks /Create` no permite directamente (como límites de tiempo de ejecución `ExecutionTimeLimit` / `max_duration_min` y política anti-solapamiento `MultipleInstancesPolicy=IgnoreNew`).

- **`schedule install`**: Registra la tarea programada para el perfil en Windows Task Scheduler (con acción `backup --unattended --profile <nombre>`):
  ```powershell
  .\bin\backup-agent.exe schedule install
  .\bin\backup-agent.exe schedule install --profile diario-r2
  ```

- **`schedule update`**: Actualiza o reinstala la tarea existente aplicando los cambios de programación más recientes definidos en `config.json`:
  ```powershell
  .\bin\backup-agent.exe schedule update
  ```

- **`schedule remove`**: Elimina la tarea del perfil en Windows Task Scheduler:
  ```powershell
  .\bin\backup-agent.exe schedule remove
  ```

- **`schedule status`**: Consulta el estado de las tareas de Windows de todos los perfiles configurados (o de uno en específico con `--profile`):
  ```powershell
  .\bin\backup-agent.exe schedule status
  ```
  *Salida de ejemplo:*
  ```text
  === Estado de tareas de Windows ===
  * full             tarea=FEMUCARIBE-Backup-full         estado=Ready
    ligero           tarea=FEMUCARIBE-Backup-ligero       estado=NO INSTALADA
  ```

##### Mecanismo de Degradación D7 (Elevación sin Errores Crudos)

Modificar el Programador de Tareas de Windows requiere privilegios elevados (**Run as Administrator**). Si el operador ejecuta `schedule install`, `update` o `remove` desde una terminal sin permisos de administrador:

1. El agente **no falla abruptamente ni arroja un error crudo de Win32**.
2. Vuelca de manera segura el XML de la tarea en `%TEMP%\FEMUCARIBE-Backup-<perfil>.xml`.
3. Informa de manera amigable al operador y muestra el **comando exacto listo para copiar y pegar** en una consola elevada:
   ```text
   Se requiere permiso de administrador para gestionar la tarea.
   Ejecutá este comando en una consola elevada (copiar y pegar):

       schtasks /Create /TN "FEMUCARIBE-Backup-full" /XML "%TEMP%\FEMUCARIBE-Backup-FEMUCARIBE-Backup-full.xml" /F

   El agente sigue operativo: solo quedó sin registrar/actualizar la tarea.
   ```
4. El proceso termina de forma limpia con código `0`, garantizando que la falta de permisos de scheduler no interrumpa flujos de trabajo mayores.

---

#### 4. Ejecutar Backup (`backup`)

```powershell
# Modo normal: respeta la idempotencia diaria para el perfil activo
.\bin\backup-agent.exe backup

# Ejecutar un perfil específico
.\bin\backup-agent.exe backup --profile diario-r2

# Modo desatendido para Task Scheduler (garantiza cero prompts o lectura de stdin)
.\bin\backup-agent.exe backup --unattended

# Forzar ejecución manual (ignora si ya corrió hoy)
.\bin\backup-agent.exe backup --force

# Usar archivo de configuración alternativo
.\bin\backup-agent.exe backup --config C:\soporte\config.custom.json --force
```

---

#### 5. Sincronización Remota de Pendientes (`sync`)

```powershell
# Sube a R2/UNC el backup pendiente registrado en state.json si hubo fallos de red previos
.\bin\backup-agent.exe sync

# Sincronizar bajo un perfil específico
.\bin\backup-agent.exe sync --profile full

# Modo desatendido
.\bin\backup-agent.exe sync --unattended

# Forzar subida del último backup registrado aunque no esté marcado como pendiente
.\bin\backup-agent.exe sync --force
```

---

#### 6. Validación de Configuración (`config validate`)

Permite validar la sintaxis y coherencia de `config.json` (perfiles, plataformas, schedules válidos y overrides) sin iniciar procesos de backup ni locks:

```powershell
.\bin\backup-agent.exe config validate
```
*Salida:* Devuelve código `0` si la configuración es íntegra y válida; devuelve código **`2`** (`ExitConfigErr`) con el detalle del error si algún parámetro es incorrecto.

---

#### 7. Consultar Estado (`status`)

```powershell
# Estado consolidado del perfil activo
.\bin\backup-agent.exe status

# Estado consolidado de un perfil específico
.\bin\backup-agent.exe status --profile diario-r2
```

---

#### 8. Consultar Logs Recientes (`logs`)

```powershell
# Muestra las últimas 20 líneas del log del día (default)
.\bin\backup-agent.exe logs

# Especificar cantidad de líneas
.\bin\backup-agent.exe logs -n 50
```

---

#### 9. Configuración por Consola (`configure`)

```powershell
# Configuración interactiva de credenciales Cloudflare R2 con DPAPI
.\bin\backup-agent.exe configure
```

---

## Códigos de Salida Centralizados (Exit Codes)

| Código | Constante         | Significado |
|:------:|-------------------|-------------|
| **`0`** | `ExitOK`          | Operación completada con éxito o backup ya realizado el día de hoy (idempotencia). |
| **`1`** | `ExitGeneralErr`  | Error general de ejecución (fallo en SQL, error de I/O, sin TTY en comando raíz). |
| **`2`** | `ExitConfigErr`   | Configuración inválida, faltante, sintaxis corrupta (`config.json`), o perfil inexistente especificado con `--profile` o `active_profile`. |
| **`3`** | `ExitPendingSync` | Backup local creado y verificado con éxito, pero la transferencia remota (R2 o Servidor Remoto UNC) falló o quedó diferida en `pending_sync`. |
| **`4`** | `ExitLocked`      | Otra instancia del agente se encuentra en ejecución para el mismo perfil (`agent-<perfil>.lock` activo con PID vivo). |

---

## Almacenamiento en Servidor Remoto / Recurso Compartido UNC (Fase 3)

### Configuración en `config.json`

Para activar la réplica secundaria hacia un servidor de almacenamiento en red o recurso compartido Windows:

```json
{
  "backup_dir": "C:\\Backups\\",
  "server": "Caproba01\\vbadilla",
  "database": "CONTABILIDAD",
  "retain": 3,
  "login_timeout_sec": 15,
  "backup_timeout_sec": 3600,
  "remote_server": {
    "enabled": true,
    "remote_path": "\\\\ServidorBackup\\Backups\\CONTABILIDAD\\",
    "keep": 10,
    "timeout_sec": 300
  }
}
```

| Parámetro | Tipo | Requerido | Descripción |
|---|---|---|---|
| `enabled` | `bool` | Sí | Activa (`true`) o desactiva (`false`) el backend de servidor remoto. |
| `remote_path` | `string` | Sí (si `enabled=true`) | Ruta UNC o directorio destino absoluto en el servidor remoto. |
| `keep` | `int` | Opcional (default `10`) | Cantidad máxima de copias históricas a conservar en el servidor remoto (mínimo 1). |
| `timeout_sec` | `int` | Opcional (default `300`) | Tiempo límite en segundos para la transferencia completa y validación de hash. |

### Rutas UNC vs. Unidades Mapeadas (`Z:\`) en Windows Task Scheduler

> [!WARNING]
> ### ⚠️ Uso Obligatorio de Rutas UNC
> 
> En entornos Windows Server y estaciones de trabajo, las unidades de red mapeadas con letra de unidad (por ejemplo `Z:\` o `X:\`) **pertenecen exclusivamente a la sesión interactiva del usuario que inició sesión**.
> 
> Cuando el agente se ejecuta de forma desatendida mediante el **Programador de Tareas de Windows (Task Scheduler)** o como servicio:
> 1. La sesión no interactiva **no monta** las unidades mapeadas del explorador.
> 2. Intentar escribir en `Z:\Backups\` resultará en un error `The system cannot find the path specified` (error 3 de Win32).
> 
> **Regla de Arquitectura**: Debe configurarse **siempre una ruta UNC válida** (ej: `\\ServidorBackup\Backups\CONTABILIDAD\`). La cuenta de servicio de Windows bajo la cual corre la tarea programada debe tener permisos NTFS y de red de lectura/escritura sobre el recurso compartido (`Share`).

### Mecánica de Copia Segura y Doble Integridad

Para evitar que una caída de red o corte eléctrico deje un backup a medio transferir o corrompa archivos existentes:

1. **Limpieza preventiva de huérfanos**: Antes de iniciar una copia, el backend detecta y purga cualquier archivo temporal huérfano (`.tmp`) perteneciente a la base de datos actual para liberar espacio.
2. **Transferencia a archivo temporal (`.bak.tmp`)**: Se escribe inicialmente bajo el sufijo temporal `CONTABILIDAD_YYYYMMDD_HHMM.bak.tmp`.
3. **Doble verificación de integridad**:
   - **Tamaño idéntico**: Valida que los bytes transferidos coincidan byte por byte con el backup local.
   - **SHA-256 por streaming**: Lee el archivo remoto temporal calculando su hash criptográfico y asegurando que coincida exactamente con el SHA-256 local verificado.
4. **Renombrado Atómico**: Únicamente cuando la verificación es 100% exitosa, el archivo temporal se renombra a su nombre definitivo `.bak`. Si la verificación falla o la red se interrumpe, el archivo temporal se destruye y el archivo definitivo jamás se crea.

### Política de Rotación y Retención (Máximo 10 Copias)

- **Aislamiento estricto**: La rotación sólo afecta a los archivos con el patrón `<DATABASE>_YYYYMMDD_HHMM.bak`. Archivos temporales (`.tmp`), archivos de 0 bytes o backups de otras bases de datos son completamente ignorados.
- **No destrucción previa**: Las copias más antiguas **nunca se eliminan antes** de que la nueva copia haya sido transferida, verificada y renombrada exitosamente.
- **Criterio de ordenamiento**: Se ordenan cronológicamente por la fecha y hora extraída del nombre (`YYYYMMDD_HHMM`). Si existen más de `keep` copias (por defecto 10), se eliminan exclusivamente las más antiguas hasta dejar exactamente 10.

### Tolerancia a Fallos, `pending_sync.server` y Recuperación

Si durante el ciclo diario de backup el servidor remoto no responde o la red falla:
1. El backup local ya completado y verificado permanece intacto y protegido.
2. El agente marca atómicamente en `state.json`:
   ```json
   "pending_sync": {
     "r2": false,
     "server": true
   }
   ```
3. El agente registra el error como `RetryableError` y finaliza con código de salida `3` (`ExitPendingSync`).
4. **Recuperación Automática**:
   - Al día siguiente o en la próxima ejecución programada de `backup`, el agente detecta el flag pendiente y reintenta transferir el backup al servidor remoto **antes** de generar el nuevo backup diario.
   - Alternativamente, los administradores pueden disparar la recuperación manual o desatendida mediante el subcomando:
     ```powershell
     .\bin\backup-agent.exe sync
     ```
     El comando `sync` es idempotente e independiente: si R2 ya estaba sincronizado, **no vuelve a subir a R2**, sino que atiende exclusivamente al backend pendiente (`server`).

---

## Observabilidad y Registro Centralizado en Supabase (Fase 4)

El agente incorpora telemetría y auditoría centralizada en la nube consumiendo la API PostgREST de **Supabase** sobre HTTPS, permitiendo monitorear el estado operativo de todas las instancias de backup en tiempo real.

### Principio de Diseño: Cero Impacto en Backups

- **Log local primario y autoritativo:** El registro estructurado en disco (`logs/agent-YYYY-MM-DD.log`) es la fuente de verdad incondicional. Supabase actúa exclusivamente como agregador secundario de observabilidad.
- **Tolerancia a fallos absoluta:** La caída de red, timeouts, errores 5xx o rate limiting de Supabase **JAMÁS cancelan ni marcan como fallido un backup local o remoto**.
- **Buffering resiliente (`pending_events`):** Todo evento que no pueda transmitirse de inmediato se almacena en disco dentro de `state.json`.
- **Vaciado y reintento automático:** Al ejecutar `agent sync` o en el siguiente ciclo de `agent backup`, los eventos acumulados se retransmiten hacia Supabase.
- **Idempotencia estricta:** Cada evento posee un identificador criptográfico único (`event_id` con formato `evt_<hex32>`). El cliente PostgREST envía la cabecera `Prefer: return=minimal,resolution=ignore-duplicates`, evitando registros duplicados o errores en caso de retransmisión.

---

### Gestión de Infraestructura con Supabase CLI

Toda la infraestructura y esquema de Supabase está estrictamente versionada en el repositorio bajo `supabase/`:

#### 1. Instalación del Supabase CLI

En Windows vía Scoop (recomendado):

```powershell
scoop bucket add supabase https://github.com/supabase/scoop-bucket.git
scoop install supabase
```

#### 2. Autenticación y Vinculación

```powershell
# Autenticarse con el Personal Access Token de la organización
supabase login

# Inicializar configuración local del repositorio (genera supabase/config.toml)
supabase init

# Vincular al proyecto oficial de FEMUCARIBE
supabase link --project-ref oxpxyiucnzpedawwkosy
```

> **Proyecto Oficial:** `oxpxyiucnzpedawwkosy` (*soporte@femucaribe.go.cr's Project*, Postgres 17.6, región `us-east-1`).

#### 3. Despliegue de Migraciones

La estructura de la base de datos se mantiene en `supabase/migrations/20260910180000_create_backup_events.sql`. Para aplicar cambios al entorno de producción:

```powershell
# Previsualizar cambios sin aplicarlos
supabase db push --project-ref oxpxyiucnzpedawwkosy --dry-run

# Aplicar migraciones directamente
supabase db push --project-ref oxpxyiucnzpedawwkosy
```

#### 4. Entorno de Desarrollo Local (Opcional con Docker)

Si se dispone de Docker Desktop en la máquina de desarrollo, es posible levantar el stack completo de Supabase en local:

```powershell
# Iniciar servicios locales (Postgres, PostgREST, Studio en http://localhost:54323)
supabase start

# Detener servicios locales
supabase stop
```

---

### Configuración del Agente

1. En `config.json`, habilitar el bloque `supabase`:
   ```json
   "supabase": {
     "enabled": true,
     "url": "https://oxpxyiucnzpedawwkosy.supabase.co",
     "timeout_sec": 10
   }
   ```
2. Configurar la clave API en la variable de entorno del sistema o de la sesión:
   ```powershell
   $env:SUPABASE_KEY="eyJhbGciOi..."
   ```

---

### Catálogo Canónico de Eventos

| Tipo de Evento | Momento de Emisión | Status Típicos |
|---|---|---|
| `agent_started` | Inicio de ejecución del comando `backup` | `RUNNING` |
| `backup_started` | Inicio de la operación `BACKUP DATABASE` en SQL Server | `RUNNING` |
| `local_backup_completed` | Backup local generado y validado con `RESTORE VERIFYONLY` | `SUCCESS` |
| `local_rotation_completed` | Poda de copias locales antiguas según política de retención | `SUCCESS` |
| `local_rotation_failed` | Fallo al podar copias locales antiguas | `FAILED` |
| `r2_sync_completed` | Archivo `.bak` sincronizado a Cloudflare R2 | `SUCCESS` |
| `r2_sync_failed` | Fallo de conexión o transferencia a Cloudflare R2 | `FAILED` |
| `server_sync_completed` | Archivo `.bak` sincronizado al Servidor Remoto / UNC | `SUCCESS` |
| `server_sync_failed` | Fallo de transferencia o validación en Servidor Remoto | `FAILED` |
| `server_rotation_completed`| Poda de copias antiguas en Servidor Remoto | `SUCCESS` |
| `server_rotation_failed` | Fallo al podar copias en Servidor Remoto | `FAILED` |
| `pending_sync` | Se marcaron transferencias diferidas para `agent sync` | `PENDING` |
| `backup_completed` | Ciclo de backup finalizado con éxito | `SUCCESS` |
| `backup_failed` | Fallo crítico en el proceso de backup (error higienizado) | `FAILED` |
| `agent_finished` | Finalización de la corrida del agente con duración total en ms | `SUCCESS` / `FAILED` |

---

### Consulta y Auditoría de Eventos

#### Vía PostgREST API (PowerShell)

```powershell
$headers = @{
    "apikey" = $env:SUPABASE_KEY
    "Authorization" = "Bearer $env:SUPABASE_KEY"
}
Invoke-RestMethod -Uri "https://oxpxyiucnzpedawwkosy.supabase.co/rest/v1/backup_events?order=timestamp.desc&limit=10" -Headers $headers
```

#### Vía Supabase Studio
Acceder al Table Editor en la consola web de Supabase para visualizar gráficos de actividad, filtrar por `status = 'FAILED'`, o auditar el rendimiento por `duration_ms`.

---

## Configuración en Windows Task Scheduler

Para la ejecución desatendida en producción, el agente ofrece dos vías de despliegue:

### 1. Despliegue Automatizado vía CLI (Recomendado)

El subcomando `schedule install` genera una definición XML robusta y la registra directamente en Windows Task Scheduler utilizando `schtasks.exe`:

```powershell
# Registrar la tarea del perfil activo (ejecutar en PowerShell como Administrador)
.\bin\backup-agent.exe schedule install

# Registrar la tarea para un perfil específico
.\bin\backup-agent.exe schedule install --profile diario-r2
```

**Ventajas de la definición XML generada por el agente:**
- **Prevención de Solapamiento:** Fija `MultipleInstancesPolicy = IgnoreNew`, evitando que se lance un segundo backup si el anterior aún continúa en ejecución.
- **Límite de Tiempo de Ejecución:** Fija `ExecutionTimeLimit` en base a `max_duration_min` para impedir que procesos congelados consuman recursos indefinidamente.
- **Disparadores Precisos:** Soporta modos `daily` (diario a una hora fija), `weekly` (días seleccionados de la semana) e `interval` (cada *N* minutos).
- **Acción Desatendida:** Configura la acción automáticamente como `backup --unattended --profile <nombre>` apuntando al ejecutable del agente.
- **Degradación D7:** Si la consola no posee privilegios elevados, el agente entrega el comando exacto `schtasks /Create /TN ... /XML ... /F` para elevar sin fallos crudos.

---

### 2. Configuración Manual mediante la Interfaz Gráfica (`taskschd.msc`)

Si se prefiere crear la tarea manualmente desde la GUI de Windows:

1. **Acción:** `Iniciar un programa`
   - **Programa o script:** `C:\Agente\backup-agent.exe`
   - **Agregar argumentos:** `backup --unattended --profile full`
   - **Iniciar en:** `C:\Agente\`
2. **Disparadores (Triggers):**
   - Disparador programado según el schedule requerido (ej: diario a las `23:00`).
   - Disparador al iniciar el sistema (*At startup*) opcional con retraso de 5 a 10 minutos para asegurar que SQL Server esté en línea.
3. **Condiciones y Configuración:**
   - Activar *"Ejecutar tanto si el usuario inició sesión como si no"*.
   - Marcar *"Ejecutar con los privilegios más altos"* si la cuenta lo requiere.
   - Cuenta de servicio con permisos en SQL Server, sistema de archivos local y DPAPI.

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

#### 3. Tests de Integración de Almacenamiento Remoto / Servidor UNC (Fase 3)
Valida el pipeline completo end-to-end simulando un servidor remoto en un filesystem aislado:
- Transferencia con `.tmp`, verificación estricta de tamaño y SHA-256 por streaming, y renombrado atómico.
- Purgado de temporales huérfanos tras interrupciones previas.
- Conservación de backups previos ante caídas de red y recuperación diferida con `app.Sync()`.
- Pipeline dual simultáneo (Cloudflare R2 + Servidor Remoto con políticas de retención independientes).

```powershell
go test -v -count=1 ./test/integration/storage/...
```

#### 4. Tests de Integración y Resiliencia de Supabase (Fase 4)
Valida el ciclo de vida de observabilidad y tolerancia a fallos:
- Transmisión exitosa de eventos operativos en tiempo real.
- Simulación de caída de servicio (HTTP 503): verificación de no bloqueo del backup y almacenamiento de eventos en `state.PendingEvents`.
- Recuperación automática y vaciado de cola mediante `app.Sync()`.
- Idempotencia estricta frente a retransmisiones duplicadas (`resolution=ignore-duplicates`).
- Verificación directa contra endpoint remoto real de Supabase.

```powershell
go test -v -count=1 ./test/integration/supabase/...
```

#### 5. Limpieza de Artefactos de Prueba
Elimina de forma segura carpetas temporales en `C:\BackupsTest\` protegiendo categóricamente `C:\Backups\`:

```powershell
.\scripts\cleanup-test.ps1
```

#### 6. Conservación de Artefactos para Diagnóstico
Si se desea inspeccionar los archivos generados tras una falla:

```powershell
$env:KEEP_TEST_ARTIFACTS="true"
go test ./...
```


