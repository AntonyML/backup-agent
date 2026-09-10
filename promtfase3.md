Claro. Para la **Fase 3** mantendría el mismo enfoque de las fases anteriores: `ServerBackend` desacoplado, primero validar la copia, después rotar, y **nunca borrar una copia anterior antes de confirmar que la nueva está íntegra**.

 Prompt — Fase 3: Copia a servidor + rotación de 10 copias

# Prompt — Fase 3: Backup Agent FEMUCARIBE (Go) — ServerBackend + Rotación de 10 copias

 IMPORTANTE: no hagas commits sin que yo diga explícitamente **"commit autorizado"**. Mostrame el diff completo de cada archivo antes de aplicar cualquier cambio. Al terminar cada bloque de trabajo, hacé un **STOP DURO** y esperá mi revisión antes de seguir.

 ## Contexto

 Las Fases 1 y 2 ya implementaron:

 - Backup local de SQL Server.
- `RESTORE VERIFYONLY`.
- SHA-256.
- Rotación local.
- `state.json`.
- Lock.
- R2 + `pending_sync`.
- DPAPI.
- CLI Cobra.
- TUI.
- `application.App`.
- Interfaz `Backend`.
- Infraestructura de testing con SQLite/SQL Server.
- Configuración de testing configurable.

 La Fase 2.1 dejó preparada la arquitectura para agregar:

```
ServerBackend
```

 sin modificar Cobra ni la TUI.

 Esta fase implementa exclusivamente:

 > **Copia del backup válido a un servidor remoto + rotación de 10 copias.**

 No implementar todavía Supabase ni nuevas funcionalidades de monitoreo.

---

 # 1\. ServerBackend

 Implementar:

```
type ServerBackend struct {
    // configuración y dependencias explícitas
}
```

 utilizando la interfaz existente:

```
type Backend interface {
    Name() string
    Upload(ctx context.Context, localPath string) error
    Rotate(ctx context.Context, keep int) error
    LatestRemote(ctx context.Context) (string, error)
}
```

 `ServerBackend` NO debe importar ni conocer:

 - `internal/state`;
- `internal/cli`;
- Bubble Tea;
- Cobra;
- R2;
- otro backend.

 Debe limitarse a copiar, verificar y rotar archivos.

---

 # 2\. Protocolo de copia

 El servidor remoto debe ser configurable.

 No asumir una ruta fija ni hardcodear una PC concreta.

 Por defecto puede existir una configuración FEMUCARIBE, pero debe poder cambiarse para otra instalación.

 Ejemplo:

```
SERVER_HOST
SERVER_SHARE
SERVER_BACKUP_PATH
```

 o la estructura que mejor encaje con Windows.

 La configuración debe tener:

```
Validate() error
```

 y validarse antes de iniciar una operación.

---

 # 3\. Windows Server / recurso compartido

 La implementación debe estar pensada principalmente para un servidor Windows accesible mediante una ruta UNC, por ejemplo:

```
\\ServidorBackup\Backups\CONTABILIDAD\
```

 pero **no hardcodear ese valor**.

 La ruta debe poder configurarse.

 Si se utiliza una unidad de red:

```
Z:\
```

 no asumir que existe en Windows Task Scheduler.

 Preferir rutas UNC porque los servicios/tareas programadas pueden no tener las mismas unidades mapeadas que una sesión interactiva.

 Documentar esta consideración.

---

 # 4\. Copia segura

 La copia debe utilizar un archivo temporal en el servidor.

 Ejemplo:

```
CONTABILIDAD_20260910_1015.bak.tmp
```

 Durante la transferencia:

```
local .bak
    ↓
server .bak.tmp
```

 Cuando la copia termine correctamente:

```
.bak.tmp
    ↓
validación
    ↓
rename
    ↓
.bak
```

 Nunca considerar el `.tmp` como backup válido.

 Nunca sobrescribir silenciosamente una copia válida existente.

---

 # 5\. Verificación de integridad

 Después de copiar el archivo al servidor:

 1. Obtener tamaño del archivo local.
2. Obtener tamaño del archivo remoto.
3. Compararlos.
4. Calcular/verificar SHA-256 cuando sea posible.
5. Solo considerar la copia exitosa cuando la integridad haya sido confirmada.

 Como mínimo:

```
size local == size remoto
```

 Y preferentemente:

```
SHA256 local == SHA256 remoto
```

 si la implementación/protocolo lo permite.

 No asumir que copiar el archivo sin error significa que la copia está íntegra.

---

 # 6\. Regla crítica de seguridad

 Nunca ejecutar:

```
copiar nueva
↓
borrar antiguas
```

 sin confirmar primero que la nueva copia está completa e íntegra.

 El orden obligatorio es:

```
Backup local válido
       ↓
Copiar al servidor
       ↓
Verificar copia
       ↓
Renombrar .tmp → .bak
       ↓
Confirmar backup remoto
       ↓
Rotar
```

 Si la copia falla:

```
backup local permanece intacto
backup remoto anterior permanece intacto
NO ejecutar rotación destructiva
```

---

 # 7\. Rotación de 10 copias

 El servidor debe conservar:

```
10 copias válidas
```

 como máximo.

 Configurable mediante:

```
keep = 10
```

 Por defecto debe ser `10`.

 Ejemplo:

```
backup-01.bak
backup-02.bak
...
backup-10.bak
backup-11.bak
```

 Después de confirmar `backup-11`:

```
backup-02 ... backup-11
```

 deben permanecer.

 La copia más antigua debe eliminarse solamente después de que la nueva haya sido confirmada.

---

 # 8\. Archivos que NO cuentan para rotación

 No considerar:

```
*.tmp
```

 como backups válidos.

 Tampoco considerar:

 - archivos corruptos;
- archivos con nombres inválidos;
- archivos que no correspondan al patrón de backup;
- archivos de otras bases;
- archivos parciales.

 Definir claramente el patrón aceptado.

 Por ejemplo:

```
CONTABILIDAD_YYYYMMDD_HHMM.bak
```

---

 # 9\. Fallos de red

 Si el servidor está inaccesible:

```
Upload()
```

 debe devolver un error clasificable como recuperable.

 La capa `application` debe decidir qué hacer con ese error.

 No modificar directamente:

```
state.json
```

 desde `ServerBackend`.

 El flujo debe ser:

```
ServerBackend
    ↓
error
    ↓
application
    ↓
RetryableError
    ↓
state repository
```

---

 # 10\. pending\_sync del servidor

 Agregar soporte equivalente a:

```
pending_sync.server
```

 al estado existente, sin romper:

```
pending_sync.r2
```

 Por ejemplo:

```
{
  "pending_sync": {
    "r2": false,
    "server": true
  }
}
```

 Si la copia al servidor falla:

```
pending_sync.server = true
```

 El backup local continúa siendo exitoso.

 En una ejecución posterior:

```
pending_sync.server == true
```

 debe provocar un intento de sincronizar el backup pendiente antes de generar un backup nuevo, siguiendo las reglas ya establecidas para R2.

 No duplicar innecesariamente la lógica de sincronización.

---

 # 11\. Qué pasa si falla la rotación

 Si:

```
Upload OK
Verify OK
Rename OK
Rotate FALLA
```

 el backup nuevo debe permanecer.

 No considerar el backup completo como perdido.

 Registrar una advertencia clara.

 En la siguiente ejecución se debe poder volver a intentar la rotación.

 Nunca borrar todas las copias por un error parcial de rotación.

---

 # 12\. Timeout

 Toda operación contra el servidor debe tener timeout mediante:

```
context.WithTimeout(...)
```

 No permitir que:

```
servidor desconectado
```

 deje el proceso de Task Scheduler bloqueado indefinidamente.

 El timeout debe ser configurable o definido mediante una configuración razonable.

---

 # 13\. Configuración

 Agregar configuración para ServerBackend.

 Ejemplo conceptual:

```
type ServerConfig struct {
    Enabled        bool
    RemotePath     string
    Keep           int
    Timeout        time.Duration
}
```

 Defaults:

```
Enabled = false
Keep = 10
```

 La ruta debe ser obligatoria cuando:

```
Enabled == true
```

 Validar:

```
ServerConfig.Validate()
```

 antes de utilizar el backend.

---

 # 14\. Integración con Application

 Integrar `ServerBackend` en la lista de backends de:

```
internal/application/
```

 sin modificar:

```
internal/cli/
internal/ui/
```

 más de lo estrictamente necesario.

 La condición de aceptación de las fases anteriores debe mantenerse:

```
agregar ServerBackend
        ↓
registrarlo en application
        ↓
Cobra continúa funcionando
TUI continúa funcionando
```

 El dashboard debe obtener automáticamente:

```
BackendStatus
```

 para Server.

 No hardcodear el servidor dentro de la TUI.

---

 # 15\. Orden de ejecución

 Mantener un orden seguro y determinista.

 Conceptualmente:

```
1. Crear backup local
2. VERIFYONLY
3. SHA-256
4. Confirmar backup local
5. Sincronizar R2 según estado existente
6. Sincronizar Server
7. Confirmar copia Server
8. Rotar Server a 10
```

 Si existe un `pending_sync.server`, respetar la política existente de pendientes y no generar innecesariamente otra copia.

 No cambiar el comportamiento de R2 salvo lo estrictamente necesario para compartir infraestructura.

---

 # 16\. Tests unitarios

 Agregar tests para:

 - configuración válida;
- configuración inválida;
- `keep = 10`;
- rotación con menos de 10 archivos;
- exactamente 10;
- más de 10;
- `.tmp` mezclados;
- archivos inválidos;
- nombres de otras bases;
- selección correcta de archivos antiguos;
- fallo de copia;
- error recuperable;
- timeout;
- verificación de tamaño;
- SHA-256;
- `pending_sync.server`.

 No utilizar el servidor real.

---

 # 17\. Tests de integración

 Crear un servidor de testing aislado.

 Preferentemente utilizar:

 - recurso compartido SMB de Docker/entorno de pruebas;
- o un servidor Windows de laboratorio;
- o una implementación de filesystem remoto apropiada para CI.

 Nunca:

```
\\servidor-produccion\...
```

 Los tests deben verificar:

```
archivo local
      ↓
copia remota
      ↓
archivo remoto
      ↓
integridad
      ↓
rename
      ↓
rotación
```

---

 # 18\. Test de interrupción

 Simular:

```
copia iniciada
     ↓
proceso interrumpido
     ↓
.tmp remoto
```

 Después:

```
siguiente ejecución
     ↓
detecta .tmp
     ↓
lo limpia
     ↓
realiza copia nueva
     ↓
verifica
     ↓
rename
```

 El `.tmp` nunca debe aparecer como backup válido.

---

 # 19\. Test de prevención de pérdida

 Probar explícitamente:

```
Backup remoto A válido
       ↓
Copiar B
       ↓
B falla
       ↓
A continúa intacto
```

 Y:

```
A válido
       ↓
B válido
       ↓
B verificado
       ↓
rotación
       ↓
A solamente se elimina si supera el límite de 10
```

 Este test es obligatorio.

---

 # 20\. Logs

 Utilizar el `log/slog` existente.

 Registrar como mínimo:

```
server backup iniciado
server backup completado
server backup verificado
server backup fallido
server rotation iniciada
server rotation completada
server rotation parcialmente fallida
server pending sync
```

 Usar campos estructurados:

```
slog.Info(
    "backup de servidor completado",
    "backend", "server",
    "file", filename,
    "size", size,
    "duration_ms", duration,
)
```

 No registrar credenciales.

---

 # 21\. Qué NO hacer

 No implementar:

 - Supabase;
- dashboard nuevo;
- cambios innecesarios de TUI;
- nuevos comandos Cobra;
- autenticación compleja;
- compresión;
- cifrado adicional del `.bak`;
- eliminación de backups locales;
- rotación R2 diferente;
- reintentos agresivos/backoff;
- credenciales hardcodeadas;
- rutas de servidor hardcodeadas.

 La fase debe concentrarse exclusivamente en:

```
ServerBackend
+
copia segura
+
verificación
+
rotación 10
+
pending_sync.server
+
tests
```

---

 # 22\. Entregables

 ## Bloque A — ServerBackend

 1. `ServerBackend`.
2. Configuración.
3. `Validate()`.
4. Copia `.tmp`.
5. Rename seguro.
6. Verificación de tamaño/hash.
7. Timeout.

 **STOP DURO. Esperar revisión.**

 ## Bloque B — Rotación

 1. Rotación de 10.
2. Ignorar `.tmp`.
3. Ignorar archivos inválidos.
4. No borrar antes de confirmar nueva copia.
5. Tests completos.

 **STOP DURO. Esperar revisión.**

 ## Bloque C — Application

 1. Integración con `application`.
2. `pending_sync.server`.
3. Clasificación `RetryableError`.
4. Orden correcto del pipeline.
5. Regresión R2/Local.

 **STOP DURO. Esperar revisión.**

 ## Bloque D — Integración

 1. Servidor de testing aislado.
2. Copia real.
3. Verificación.
4. Rotación real.
5. Simulación de interrupción.
6. Recuperación.
7. Tests completos.

 **STOP DURO. Esperar revisión.**

 ## Bloque E — Documentación

 Actualizar `README.md` con:

 - configuración del servidor;
- ruta UNC;
- recomendación de UNC frente a unidades mapeadas para Task Scheduler;
- configuración de 10 copias;
- cómo ejecutar tests;
- comportamiento de `pending_sync.server`;
- recuperación ante servidor inaccesible.

 **STOP DURO. Esperar revisión.**

---

 # Condición de aceptación final

 La Fase 3 está terminada cuando:

```
Backup local válido
       ↓
Copia Server válida
       ↓
Integridad confirmada
       ↓
Máximo 10 copias
```

 y ante cualquier fallo:

```
backup remoto anterior
        +
backup local válido
```

 permanezcan protegidos.

 Además:

 **Agregar o modificar ServerBackend no debe requerir introducir lógica de servidor dentro de Cobra ni dentro de Bubble Tea.**

 No hacer commits.

 Mostrar el diff completo de cada archivo antes de aplicar cambios.

 Después de cada bloque hacer **STOP DURO** y esperar mi revisión.