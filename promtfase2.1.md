# Prompt — Fase 2.1 (v2): Backup Agent FEMUCARIBE (Go) — CLI con Cobra, capa de aplicación, Backend desacoplado

IMPORTANTE: no hagas commits sin que yo diga explícitamente "commit autorizado". Mostrame el diff completo de cada archivo antes de aplicar cualquier cambio. Al terminar cada bloque de trabajo, hacé un stop duro y esperá mi revisión antes de seguir con el siguiente.

## Contexto

Fase 1 (backup local + rotación) y Fase 2 (subida a R2 + rotación, `agent configure` con DPAPI) ya están implementadas. Esta fase es un **refactor transversal** antes de construir la Fase 3 (servidor), para que el tercer backend de almacenamiento se sume sin duplicar código ni tocar CLI/menú.

No agrega funcionalidad de negocio nueva — reestructura lo que ya existe.

## Arquitectura objetivo

```
cmd/backup-agent/
    main.go                 # solo inicializa la app y ejecuta el CLI

internal/
    cli/                     # Cobra vive exclusivamente acá — adaptador delgado
        root.go
        configure.go
        backup.go
        status.go
        logs.go
        sync.go
        interactive.go

    application/             # casos de uso — sin saber nada de Cobra ni de stdin/TTY
        backup.go
        sync.go

    storage/
        backend.go            # interfaz Backend
        local/
        r2/

    rotation/
    config/
    logging/
    state/
```

**Regla dura:** `internal/application/` no debe importar `github.com/spf13/cobra` ni leer de `os.Stdin` en ningún punto. Los comandos Cobra y el menú interactivo son dos "adaptadores" distintos que llaman a los mismos servicios de `application/`.

## 1. CLI con Cobra — sin ambigüedad de comportamiento

Migrar el CLI a `github.com/spf13/cobra`. Cobra es responsable únicamente de: definir el comando raíz y subcomandos, parsear flags, mostrar `--help`, y devolver códigos de salida. **Nunca** debe contener lógica de negocio dentro de un `RunE` — el handler solo traduce argumentos a una llamada al servicio de aplicación correspondiente:

```go
func newBackupCommand(app *application.App) *cobra.Command {
    cmd := &cobra.Command{
        Use:   "backup",
        Short: "Ejecuta un backup",
        RunE: func(cmd *cobra.Command, args []string) error {
            return app.Backup(cmd.Context())
        },
    }
    return cmd
}
```

Subcomandos esperados:

```
agent configure
agent backup [--unattended]
agent status
agent logs
agent sync [--unattended]
agent interactive
```

### Comportamiento del comando raíz sin subcomando

- Si hay TTY (`term.IsTerminal(int(os.Stdin.Fd()))`) → iniciar el menú interactivo.
- Si NO hay TTY → mostrar `--help` y terminar con código de salida distinto de cero, **sin bloquear esperando stdin**.

**No usar la ausencia de TTY como señal para ejecutar el backup automáticamente.** Eso es justamente lo que había que evitar: que `agent.exe` sin argumentos signifique "menú" en una sesión manual y "backup" bajo Task Scheduler — el mismo binario cambiando de semántica según el entorno es una fuente de bugs silenciosos. Para automatización, el comando debe ser siempre explícito:

```
agent backup --unattended
agent sync --unattended
```

### Modo unattended

Con `--unattended` presente (flag del comando, no global):
- Nunca mostrar menú, nunca leer stdin, nunca esperar interacción.
- Ejecutar únicamente la operación pedida por ese comando específico.
- Toda la información necesaria sale de config persistida (DPAPI), flags o estado — nunca de un prompt.

Este es el comando que va a usar Windows Task Scheduler: `agent backup --unattended`.

### Modo interactivo

Cuando hay TTY real, `agent` (sin subcomando) o `agent interactive` muestra:

```
> Configurar credenciales
  Ejecutar backup ahora
  Ver estado
  Ver logs recientes
  Forzar sincronización pendiente
  Salir
```

El menú debe invocar **los mismos servicios de `internal/application/`** que usan los comandos Cobra — no puede haber lógica de backup duplicada entre `interactive.go` y `backup.go`. Ambos caminos terminan en la misma función de aplicación.

### Exit codes centralizados

Definir y documentar una convención (por ejemplo):

```
0 = éxito
1 = error general
2 = configuración inválida
3 = error recuperable / sincronización pendiente
```

Los valores exactos son negociables, pero deben decidirse en un único punto de la capa `cli/`, nunca con `os.Exit()` disperso por el resto del código.

## 2. Interfaz `Backend` — desacoplada de estado y configuración global

```go
type Backend interface {
    Name() string
    Upload(ctx context.Context, localPath string) error
    Rotate(ctx context.Context, keep int) error
    LatestRemote(ctx context.Context) (string, error)
}
```

Restricción importante: **`Backend` no debe conocer configuración global, CLI, logging global, estado (`state.json`) ni la implementación concreta de otro backend.** Un `Backend` reporta errores — no decide qué significan operacionalmente ni toca `pending_sync` directamente.

Quién decide eso es la capa de aplicación:

```
Backend → error → application pipeline → RetryableError → state repository → pending_sync
```

Es decir: si `R2Backend.Upload()` falla, devuelve un error (tipado, ver abajo). Es `internal/application/backup.go` (o `sync.go`) quien interpreta ese error, decide si es recuperable, y es el único que llama a algo como `stateRepo.MarkPending("r2", true)`. `R2Backend` nunca importa ni conoce `internal/state`.

Definí un tipo de error propio para esta clasificación:

```go
type RetryableError struct {
    Err error
}
```

para que la capa de aplicación pueda usar `errors.As` y decidir recuperable vs. fatal sin `if` sueltos repetidos en cada backend.

## 3. Logging estructurado

`log/slog` (librería estándar, sin agregar zap/zerolog). Niveles `Debug/Info/Warn/Error`, output al log rotativo diario ya existente, legible con `Get-Content -Wait` desde PowerShell. Eventos clave con campos estructurados (`slog.Info("backup completado", "backend", "r2", "duracion_ms", 1234)`), no strings concatenados.

## 4. Validaciones

Cada struct de configuración debe tener `Validate() error`, corrido antes de tocar SQL Server o red — falla rápido con mensaje claro. Errores envueltos con `fmt.Errorf("...: %w", err)` en cada capa para poder distinguir con `errors.Is`/`errors.As`.

## Qué NO hacer en esta fase

- No tocar el comportamiento observable de negocio de Fase 1/2 (cuándo hacer backup, cuándo rotar, cuándo sincronizar) — solo cambia la organización del código.
- No implementar todavía `ServerBackend` (Fase 3) — pero la interfaz debe quedar lista para que esa fase la implemente sin tocar Cobra ni el menú.
- No agregar dependencias de logging externas — `log/slog` alcanza.
- No usar `os.Exit()` fuera de la capa `cli/`.
- No dejar que ningún `Backend` importe `internal/state`, `internal/cli`, ni conozca la existencia de otro backend.
- No usar variables globales mutables para config, estado o dependencias — pasarlas explícitamente (constructor injection).

## Tests que quiero incluidos en esta fase

CLI:
- `agent --help` termina correctamente sin leer stdin.
- `agent backup --unattended` nunca lee stdin y ejecuta el pipeline sin interacción.
- El modo interactivo solo arranca cuando hay TTY real.
- El menú interactivo y los comandos Cobra invocan los mismos servicios de `application/` (verificable por código, no solo por comportamiento observado).
- Un error de configuración inválida produce el exit code correspondiente; un error recuperable produce el suyo.
- Ningún comando Cobra contiene lógica específica de `LocalBackend`/`R2Backend` (revisable en el diff, no solo con test).
- Ningún servicio de `application/` depende de Cobra ni lee de `os.Stdin`/`os.Stdout` directamente — los tests de negocio deben poder correr sin crear un `cobra.Command` ni un TTY real.

Backend / rotación / DPAPI (ya existentes, no deben regresionar):
- Mismos tests unitarios de `LocalBackend` y `R2Backend` que ya pasaban antes del refactor.
- Confirmar que `Backend.Upload()`/`Rotate()` nunca tocan `state.json` directamente — eso solo lo hace la capa de aplicación tras interpretar el resultado.

## Condición de aceptación explícita

**Agregar `ServerBackend` en Fase 3 no debe requerir modificar ningún comando Cobra existente ni el menú interactivo — solo implementar la interfaz `Backend` y registrarla en la lista que arma la capa de aplicación.** Si esto se cumple, la abstracción quedó bien hecha.

## Dependencias nuevas a agregar

```
github.com/spf13/cobra
golang.org/x/term
```

(Si más adelante se quiere un menú con flechas/selección visual más elaborado, esa es una decisión de librería de UI separada — Cobra es el router de comandos, no la librería del menú.)

## Qué quiero como entregable de este bloque

1. `internal/cli/` con Cobra: comando raíz + subcomandos (`configure`, `backup`, `status`, `logs`, `sync`, `interactive`), todos como adaptadores delgados.
2. `internal/application/` con los casos de uso (`Backup`, `Sync`, etc.), sin dependencia de Cobra ni de stdin/TTY.
3. `Backend` refactorizado según la restricción de desacoplamiento (sin conocer `state`, config global ni otros backends); `RetryableError` u equivalente para la clasificación recuperable/fatal, decidida en `application/`.
4. Logging migrado a `log/slog`; validaciones `Validate()` en cada config.
5. `README.md` actualizado: comandos disponibles, cómo usar `--unattended` para Task Scheduler, cómo entrar al modo interactivo, y la convención de exit codes.

Detené ahí y esperá mi revisión antes de arrancar la Fase 3 (implementar `ServerBackend` sobre esta interfaz).