# Prompt — Fase 2.2: Backup Agent FEMUCARIBE (Go) — TUI (Bubble Tea + Bubbles + Lip Gloss + Glamour)

IMPORTANTE: no hagas commits sin que yo diga explícitamente "commit autorizado". Mostrame el diff completo de cada archivo antes de aplicar cualquier cambio. Al terminar cada bloque de trabajo, hacé un stop duro y esperá mi revisión antes de seguir con el siguiente.

## Contexto

Fase 2.1 ya dejó la arquitectura separada en capas: `internal/cli/` (Cobra, adaptador delgado), `internal/application/` (casos de uso, sin dependencia de Cobra ni stdin), `internal/storage/` (interfaz `Backend` desacoplada de estado y config global). Esta fase reemplaza el menú interactivo de texto plano de 2.1 por un **dashboard TUI real**, sin tocar ninguna de esas capas inferiores.

El comportamiento de `agent backup --unattended` para Task Scheduler **no cambia en absoluto** — esta fase es exclusivamente sobre la experiencia de `agent` / `agent interactive` cuando hay TTY.

## Stack a incorporar

```
github.com/charmbracelet/bubbletea   (v2 — generación actual, ver nota de versiones abajo)
github.com/charmbracelet/bubbles     (v2)
github.com/charmbracelet/lipgloss    (v2)
github.com/charmbracelet/glamour     (v2)
```

**Nota de versiones:** el ecosistema Charm avanzó a una generación v2 (`bubbletea/v2`, `bubbles/v2`, `lipgloss/v2`, `glamour/v2`). Fijá explícitamente esa generación en `go.mod` y no mezcles paquetes v1 con v2 — verificá las rutas de import reales antes de agregarlas, porque el path del módulo puede diferir entre generaciones.

## Regla arquitectónica no negociable

Los modelos de Bubble Tea (`tea.Model`) **nunca** deben importar ni conocer tipos concretos de `internal/storage/` (`R2Backend`, `LocalBackend`, futuro `ServerBackend`) ni de `internal/state` (`StateRepository`) ni conexiones SQL directas. Bubble Tea es una capa de presentación más, exactamente igual que Cobra en 2.1 — habla con `internal/application/` y recibe **DTOs simples**, no objetos de infraestructura.

Definí DTOs de presentación en `internal/application/` (o un paquete propio `internal/application/dto/` si preferís separarlo), algo como:

```go
type BackupStatus struct {
    LastRun   time.Time
    Duration  time.Duration
    Result    string // "success" | "error" | "never_run"
}

type BackendStatus struct {
    Name          string
    Configured    bool
    LastSyncOK    bool
    PendingSync   bool
}

type LogEntry struct {
    Time    time.Time
    Level   string
    Message string
}
```

El flujo correcto es:

```
Bubble Tea model → application.App (mismo servicio que usan Cobra e interactive de 2.1) → DTO → render con Bubbles/Lip Gloss
```

Nunca:

```
Bubble Tea model → R2Backend → S3
```

## Estructura de proyecto

```
internal/
    ui/
        app.go            # tea.Model raíz, orquesta las pantallas
        dashboard.go      # pantalla principal
        backup_progress.go
        status.go
        logs.go            # usa bubbles/viewport
        configure.go       # usa bubbles/textinput
        sync.go
        styles.go          # definiciones Lip Gloss centralizadas
        content/           # archivos .md para Glamour
            about.md
            backup-help.md
            configuration-help.md
            troubleshooting.md
```

`internal/cli/interactive.go` (de Fase 2.1) pasa a simplemente lanzar `tea.NewProgram(ui.NewApp(app))` — no debe seguir teniendo su propio loop de menú manual.

## 1. Pantalla principal (dashboard)

Reemplaza el menú de texto de 2.1 por un dashboard con estado real, usando `bubbles/list` o un layout custom con Lip Gloss:

```
╭────────────────────────────────────────────────────────────╮
│ FEMUCARIBE BACKUP AGENT                         v2.1       │
├────────────────────────────────────────────────────────────┤
│  BACKUP STATUS                                              │
│  Last backup     <fecha real de BackupStatus>               │
│  Duration        <duración real>                            │
│  Result          ● SUCCESS / ERROR / NEVER RUN              │
│                                                              │
│  BACKENDS                                                   │
│  ● Local          OK                                        │
│  ● R2             OK / PENDING / ERROR                      │
│  ○ Server         Not configured                            │
├────────────────────────────────────────────────────────────┤
│  [B] Backup   [S] Status   [L] Logs   [Y] Sync   [Q] Quit  │
╰────────────────────────────────────────────────────────────╯
```

Los datos del dashboard vienen de una llamada a `application.App` al entrar a la pantalla (y se refrescan tras cada operación) — no son valores hardcodeados ni mock.

## 2. Progreso de backup (asíncrono)

Al ejecutar backup desde la TUI, debe mostrarse progreso real por etapa (SQL Server, Local, R2), no solo un spinner genérico:

```
╭────────────────────────────────────────────────────────────╮
│ BACKUP IN PROGRESS                                          │
├────────────────────────────────────────────────────────────┤
│  SQL Server        ████████████████████████████  Done       │
│  Local backend     ████████████████████████████  Done       │
│  R2 backend        ███████████████░░░░░░░░░░░  52%          │
│                                                              │
│  Uploading backup...                    ◌                   │
╰────────────────────────────────────────────────────────────╯
```

Implementación: la operación de backup corre como un `tea.Cmd` (Bubble Tea ejecuta la llamada a `application.App.Backup()` en background y envía mensajes de progreso al modelo vía canal o callbacks) — **nunca bloquear el event loop de Bubble Tea con una llamada síncrona larga**. Usá `bubbles/progress` y `bubbles/spinner` para el render.

Si `application.App` hoy no expone progreso incremental (solo devuelve éxito/error al final), está bien para esta fase mostrar spinner + resultado final por etapa sin porcentaje granular — no inventes progreso falso. Documentá esa limitación si aplica.

## 3. Logs (viewport)

Pantalla de logs usando `bubbles/viewport` para scroll sobre las últimas N líneas del log del día — reemplaza al "tail" de texto plano de 2.1. Los datos vienen de leer el archivo de log real (mismo que ya existe), no de una fuente nueva.

## 4. Configuración (textinput)

Pantalla de configuración de credenciales usando `bubbles/textinput` en modo password/oculto para los campos sensibles (Access Key, Secret Key) — el flujo de guardado sigue siendo el mismo de Fase 2 (cifrado DPAPI vía `application.App`), esta fase solo cambia cómo se capturan los valores en pantalla.

## 5. Estilos (Lip Gloss)

Centralizar todos los estilos en `internal/ui/styles.go`, con convención semántica de color:

- verde → éxito
- amarillo → warning / pendiente de sincronización
- rojo → error
- azul → información
- gris → información secundaria

No dispersar `lipgloss.NewStyle()` sueltos por cada archivo de pantalla — definir constantes/estilos reutilizables una sola vez.

## 6. Ayuda contextual (Glamour)

Contenido de ayuda como Markdown en `internal/ui/content/*.md`, renderizado con Glamour en una pantalla de ayuda navegable (ej. tecla `?` o `[H] Help` en el dashboard). Mínimo: `about.md`, `backup-help.md`, `configuration-help.md`, `troubleshooting.md` (con al menos una entrada práctica: qué significa que R2 quede en estado "pending sync" y qué hacer). Esto reemplaza strings de ayuda hardcodeados en Go por contenido mantenible como texto plano.

## Qué NO hacer en esta fase

- No tocar `internal/application/`, `internal/storage/`, `internal/state/` salvo para agregar los DTOs de presentación — la lógica de negocio no cambia.
- No modificar el comportamiento de `agent backup --unattended` ni ningún otro comando Cobra existente.
- No dejar que ningún modelo de Bubble Tea importe tipos de `internal/storage/` o `internal/state/` directamente.
- No mezclar versiones v1/v2 del stack Charm.
- No bloquear el event loop de Bubble Tea con llamadas síncronas largas (backup, sync) — deben correr como `tea.Cmd` async.
- No hardcodear textos largos de ayuda en Go — van en los `.md` de `content/`.

## Tests que quiero incluidos en esta fase

- Los modelos de `internal/ui/` se pueden testear con `teatest` (o el approach de testing que use la versión de Bubble Tea elegida) sin necesitar backends reales — inyectando un `application.App` con resultados simulados.
- Verificar que el dashboard renderiza correctamente los tres estados de un backend (`OK`, `PENDING`, `ERROR`, `Not configured`).
- Verificar que la navegación entre pantallas (dashboard → backup progress → dashboard, dashboard → logs, dashboard → configure) no deja el modelo en un estado inconsistente.
- Test estático/de código (revisión de imports): ningún archivo bajo `internal/ui/` importa `internal/storage/*` o `internal/state`.

## Condición de aceptación explícita

**Agregar `ServerBackend` en Fase 3 no debe requerir modificar ningún archivo de `internal/ui/`** — el dashboard debe mostrar el tercer backend automáticamente en cuanto `application.App` lo exponga como un `BackendStatus` más, sin cambios en la capa de presentación.

## Qué quiero como entregable de este bloque

1. `internal/ui/` completo: dashboard, progreso de backup, logs (viewport), configuración (textinput), ayuda (Glamour), estilos centralizados (Lip Gloss).
2. `internal/cli/interactive.go` simplificado a lanzar el programa de Bubble Tea.
3. DTOs de presentación en `internal/application/` (o subpaquete), sin fugas de tipos de `storage`/`state` hacia `ui`.
4. `README.md` actualizado con capturas o descripción de las pantallas y cómo navegar la TUI.

Detené ahí y esperá mi revisión antes de arrancar la Fase 3 (`ServerBackend`), que debería integrarse sin tocar ni Cobra ni esta TUI.