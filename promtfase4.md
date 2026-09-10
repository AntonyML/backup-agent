Prompt — Fase 4: Backup Agent FEMUCARIBE (Go) — Logging de eventos a Supabase

IMPORTANTE: no hagas commits sin que yo diga explícitamente "commit autorizado". Mostrame el diff completo de cada archivo antes de aplicar cualquier cambio. Al terminar cada bloque de trabajo, hacé un STOP DURO y esperá mi revisión.

Contexto

Las Fases 1, 2, 2.1, 2.2, 2.3 y 3 ya están implementadas.

Actualmente el agente tiene:

Backup local.
ServerBackend.
R2Backend.
Rotación.
state.json.
pending_sync.
CLI Cobra.
TUI Bubble Tea.
Logging local con log/slog.
Infraestructura de testing.
SQLite para tests.
SQL Server aislado para integración.
Configuración desacoplada.
Arquitectura basada en application/.
Backends desacoplados mediante Backend.

Esta fase agrega únicamente:

Registro de eventos operativos del backup en Supabase.

El log local continúa siendo la fuente principal para diagnóstico detallado.

Supabase será utilizado como registro central de eventos, no como reemplazo del logging local.

1. IMPORTANTE — utilizar la infraestructura Supabase existente

NO crear un proyecto nuevo de Supabase.

NO inventar tablas, funciones, configuraciones o infraestructura sin antes revisar qué existe actualmente.

El proyecto Supabase existente es:

Project Ref:
oxpxyiucnzpedawwkosy


Antes de implementar cualquier código relacionado con Supabase, utilizar el CLI oficial:

supabase login


Luego:

supabase init


si el proyecto todavía no tiene la estructura local de Supabase.

Después enlazar:

supabase link --project-ref oxpxyiucnzpedawwkosy

Regla importante

Una vez enlazado el proyecto:

INSPECCIONAR primero la infraestructura existente.

Revisar:

supabase/
supabase/config.toml
supabase/migrations/


y utilizar el CLI para conocer el estado remoto.

Por ejemplo, utilizar los comandos apropiados del CLI para:

revisar migraciones;
comparar esquema local/remoto;
detectar tablas existentes;
detectar funciones existentes;
revisar la estructura actual.

No asumir que la base está vacía.

No crear una tabla events o logs automáticamente sin comprobar primero qué existe.

La infraestructura existente en Supabase es la fuente de verdad.

2. Objetivo

Registrar eventos importantes del Backup Agent en Supabase.

Ejemplos:

backup_started
backup_completed
backup_failed

server_upload_started
server_upload_completed
server_upload_failed

r2_upload_started
r2_upload_completed
r2_upload_failed

rotation_completed
rotation_failed

pending_sync
sync_completed

agent_started
agent_finished


Los nombres exactos pueden adaptarse al esquema existente.

3. No duplicar infraestructura

Si Supabase ya tiene una tabla que pueda representar eventos de agentes:

UTILIZARLA.

Si existe una estructura equivalente:

backup_events
agent_events
backup_logs
events


adaptarse a ella.

Solo crear una nueva migración si realmente no existe una estructura adecuada.

Antes de crearla:

inspeccionar el esquema;
mostrar qué existe;
determinar qué falta;
proponer la migración mínima;
detenerse para revisión.

No crear infraestructura innecesaria.

4. CLI de Supabase como herramienta principal

El flujo de desarrollo debe utilizar el CLI oficial.

Ejemplo:

supabase login
supabase link --project-ref oxpxyiucnzpedawwkosy


Para cambios de esquema:

supabase migration new <nombre>


y aplicar/verificar mediante el flujo estándar del CLI.

No modificar directamente la base remota mediante SQL manual si el cambio debe quedar versionado.

Toda modificación estructural debe quedar representada mediante:

supabase/migrations/


para que el entorno pueda reproducirse.

5. NO utilizar el CLI desde el Backup Agent

IMPORTANTE:

El comando:

supabase login


es para desarrollo/administración de infraestructura.

El binario:

agent.exe


NO debe ejecutar:

supabase login
supabase link
supabase db push
supabase migration ...


en runtime.

El agente solamente debe comunicarse con Supabase mediante una API/cliente apropiado.

6. Cliente Supabase

Elegir la forma más simple y mantenible para que Go registre eventos.

Antes de agregar una dependencia:

verificar las opciones oficiales/compatibles;
revisar compatibilidad con el proyecto actual;
evitar agregar un SDK pesado si una llamada HTTP bien encapsulada es suficiente.

Crear una abstracción:

type EventRepository interface {
    Append(ctx context.Context, event Event) error
}


La aplicación no debe conocer detalles HTTP ni Supabase.

7. Modelo de evento

Crear un modelo interno desacoplado:

type Event struct {
    EventID    string
    Timestamp  time.Time
    EventType  string
    Status     string
    Backend    string
    Hostname   string
    Database   string
    FileName   string
    SizeBytes  int64
    DurationMs int64
    Error      string
}


Los campos exactos deben adaptarse al esquema Supabase existente.

No enviar información sensible.

8. Qué NO registrar

Nunca enviar a Supabase:

Access Key de R2;
Secret Key;
credenciales SQL;
passwords;
tokens;
DPAPI blobs;
contenido del backup;
datos contables de los registros;
información sensible innecesaria.

El evento debe contener metadatos operativos, no datos de negocio.

9. Arquitectura

Mantener:

application
     │
     ▼
EventRepository
     │
     ▼
SupabaseEventRepository
     │
     ▼
Supabase


No:

TUI → Supabase
CLI → Supabase
Backend → Supabase


Los backends siguen sin conocer Supabase.

Por ejemplo:

ServerBackend
     ↓
error
     ↓
application
     ↓
EventRepository
     ↓
Supabase

10. Logging local + Supabase

El log local continúa funcionando exactamente como hasta ahora.

Cuando ocurre un evento importante:

                    ┌──→ slog → archivo local
application event ──┤
                    └──→ EventRepository → Supabase


Si Supabase está caído:

backup
  ↓
OK
  ↓
Supabase
  ↓
FAIL


El backup NO debe considerarse fallido por no poder registrar el evento en Supabase.

El logging remoto es secundario.

11. Pending events

Agregar estado para eventos pendientes, solamente si la arquitectura existente lo necesita.

Por ejemplo:

pending_events


o una estructura equivalente.

No inventar un sistema paralelo si ya existe una abstracción de sincronización reutilizable.

Si Supabase no está disponible:

evento local
    ↓
persistir pendiente
    ↓
próxima ejecución
    ↓
intentar enviar


El mecanismo debe evitar perder eventos importantes.

12. Idempotencia

Los eventos deben tener un identificador único.

Ejemplo:

EventID string


El mismo evento no debe insertarse dos veces si una operación de red devuelve un timeout después de que Supabase haya recibido la solicitud.

Utilizar una estrategia de idempotencia apropiada, por ejemplo:

UNIQUE(event_id)


si el esquema existente lo permite.

No depender únicamente de:

timestamp + event_type


para detectar duplicados.

13. Eventos mínimos obligatorios

Registrar como mínimo:

Agent
agent_started
agent_finished

Backup
backup_started
backup_completed
backup_failed

Local
local_backup_completed
local_rotation_completed
local_rotation_failed

R2
r2_sync_completed
r2_sync_failed

Server
server_sync_completed
server_sync_failed
server_rotation_completed
server_rotation_failed

Estado
pending_sync


Los nombres finales deben adaptarse a la infraestructura existente.

14. Información útil del evento

Cuando sea relevante, registrar:

timestamp
event_type
status
backend
hostname
database
filename
size_bytes
duration_ms
error_code
error_message
agent_version


No registrar stack traces gigantes ni información sensible.

Para errores, guardar un mensaje útil pero sanitizado.

15. Versionado del agente

Agregar al evento:

agent_version


utilizando la versión real del binario.

No hardcodear una versión diferente en cada evento.

Si actualmente el proyecto no tiene mecanismo de versión, crear uno simple y centralizado.

16. Autenticación

No colocar credenciales Supabase directamente en:

config.json


ni en el código.

Determinar primero qué mecanismo de autenticación corresponde al esquema existente.

Para un agente instalado en una máquina Windows, priorizar un mecanismo seguro y apropiado para runtime.

Si se requiere una credencial:

almacenarla de manera segura;
no escribirla en logs;
no enviarla a eventos;
no incluirla en Git.

No asumir que una service_role key debe distribuirse sin protección.

No implementar autenticación antes de revisar la infraestructura existente y definir exactamente qué necesita el cliente Go.

17. Configuración

Agregar configuración:

type SupabaseConfig struct {
    Enabled  bool
    URL      string
    // mecanismo de autenticación según infraestructura existente
    Timeout  time.Duration
}


Defaults:

Enabled = false


La configuración debe ser opcional.

Si Supabase no está configurado:

backup continúa normalmente
logging local continúa normalmente

18. Timeout

Toda comunicación con Supabase debe utilizar:

context.WithTimeout(...)


No permitir que un problema de red bloquee:

agent backup --unattended


El timeout debe ser configurable o tener un default razonable.

19. Error handling

Los errores de Supabase deben clasificarse correctamente.

Ejemplo:

HTTP 401/403
→ configuración/autenticación

HTTP 429
→ recuperable

timeout
→ recuperable

network unavailable
→ recuperable

schema inválido
→ error de configuración/desarrollo


No convertir automáticamente todos los errores en fatal.

El backup local/remoto no debe fallar solamente porque falló el registro del evento.

20. Tests

Crear tests unitarios para:

serialización de Event;
generación de EventID;
idempotencia;
timeout;
error de red;
HTTP 401;
HTTP 500;
HTTP 429;
Supabase deshabilitado;
evento enviado correctamente;
evento pendiente;
reintento posterior.

Utilizar un HTTP server/mock local.

Nunca utilizar el proyecto Supabase real en los tests unitarios.

21. Tests de integración

Si se necesita probar Supabase realmente:

utilizar primero la infraestructura local de Supabase mediante el CLI:

supabase start


y ejecutar las pruebas contra esa instancia local.

No ejecutar tests destructivos contra:

oxpxyiucnzpedawwkosy


de producción.

Para pruebas contra el proyecto remoto existente, limitarse a operaciones controladas y explícitamente autorizadas.

22. Migraciones

Si es necesario modificar el esquema:

supabase/migrations/


debe contener la migración.

Antes de aplicarla:

inspeccionar esquema actual;
crear migración mínima;
mostrar diff;
probar localmente;
detenerse para revisión.

No hacer:

DROP TABLE


ni cambios destructivos sobre tablas existentes sin autorización explícita.

23. TUI

La TUI debe mostrar el estado de Supabase si la capa application ya expone información suficiente.

Por ejemplo:

Supabase   ● Connected
Supabase   ● Pending
Supabase   ○ Disabled
Supabase   ● Error


Pero:

no implementar llamadas Supabase directamente desde Bubble Tea.

La TUI únicamente consume DTOs de application.

24. CLI

No agregar comandos innecesarios.

Si se considera útil:

agent sync


puede sincronizar eventos pendientes además de los backends existentes, pero solo si encaja con la arquitectura actual.

No crear comandos duplicados únicamente para Supabase.

25. Qué NO hacer

No:

crear un nuevo proyecto Supabase;
crear tablas sin inspeccionar primero;
inventar una estructura paralela a la existente;
usar supabase login desde el agente;
usar supabase link desde el agente;
ejecutar migraciones desde runtime;
enviar secretos;
enviar datos contables;
bloquear un backup por caída de Supabase;
conectar directamente TUI/CLI con Supabase;
modificar LocalBackend/R2Backend/ServerBackend para que conozcan Supabase.
26. Entregables
Bloque A — Inspección Supabase
Inicializar/enlazar CLI si corresponde.
supabase login.
supabase init si corresponde.
supabase link --project-ref oxpxyiucnzpedawwkosy.
Inspeccionar migraciones y esquema existente.
Identificar la estructura que debe utilizarse.
NO modificar todavía la base.

STOP DURO. Esperar revisión.

Bloque B — EventRepository
Modelo Event.
EventRepository.
Implementación Supabase.
Configuración.
Timeout.
Manejo de errores.
Tests con servidor/mock local.

STOP DURO. Esperar revisión.

Bloque C — Integración
Integrar eventos desde application.
Mantener slog local.
Eventos de Local/R2/Server.
Idempotencia.
Pending events si son necesarios.
No romper ningún backend existente.

STOP DURO. Esperar revisión.

Bloque D — Supabase local
supabase start.
Aplicar migraciones.
Ejecutar integración.
Verificar eventos.
Probar duplicados.
Probar caída de Supabase.
Probar recuperación.

STOP DURO. Esperar revisión.

Bloque E — Documentación

Actualizar README.md con:

instalación del Supabase CLI;
supabase login;
supabase init;
supabase link;
proyecto utilizado;
estructura de migraciones;
cómo levantar Supabase local;
configuración del agente;
eventos registrados;
comportamiento cuando Supabase está caído;
cómo revisar eventos.

STOP DURO. Esperar revisión.

Condición de aceptación final

La Fase 4 debe cumplir:

Backup
  │
  ├──→ log local (obligatorio)
  │
  └──→ Supabase (centralización)


Si Supabase está disponible:

evento → Supabase → OK


Si Supabase está caído:

evento → pendiente/local
backup → CONTINÚA


Cuando vuelva a estar disponible:

pendiente → Supabase


sin duplicar eventos.

La infraestructura Supabase debe quedar versionada mediante el CLI y migraciones, utilizando el proyecto existente:

oxpxyiucnzpedawwkosy


y sin inventar una arquitectura paralela.

No hacer commits.

Mostrar el diff completo antes de aplicar cambios.

Después de cada bloque hacer STOP DURO y esperar mi revisión.