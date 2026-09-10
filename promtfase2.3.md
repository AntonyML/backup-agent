Sí. Con los prompts de Fase 1, 2, 2.1 y 2.2 como contexto, el **Prompt 2.3** debería introducir una infraestructura de pruebas que permita validar el comportamiento completo del agente **sin tocar nunca la base `CONTABILIDAD` de producción**, y además comprobar específicamente backup, restauración/verificación, corrupción, rotación y recuperación.

 Te lo dejo listo para copiar/pegar:

 # Prompt — Fase 2.3: Backup Agent FEMUCARIBE (Go) — Infraestructura de Testing con SQLite + SQL Server aislado

 IMPORTANTE: no hagas commits sin que yo diga explícitamente **"commit autorizado"**. Mostrame el diff completo de cada archivo antes de aplicar cualquier cambio. Al terminar cada bloque de trabajo, hacé un **stop duro** y esperá mi revisión antes de seguir con el siguiente.

 ## Contexto

 Las Fases 1, 2, 2.1 y 2.2 ya establecieron:

 - Backup local de SQL Server.
- `RESTORE VERIFYONLY`.
- Rotación de backups.
- SHA-256.
- `state.json`.
- Lock files.
- Sincronización con R2.
- DPAPI para secretos reales.
- Arquitectura basada en `application/`.
- Interfaz desacoplada `Backend`.
- CLI con Cobra.
- TUI con Bubble Tea.
- Logging con `log/slog`.
- Configuración validada mediante `Validate()`.
- Backends desacoplados de estado y configuración global.

 Esta fase **NO agrega funcionalidad de negocio para producción**.

 El objetivo exclusivo de esta fase es construir una **infraestructura completa de testing aislada**, que permita comprobar que el agente:

 1. Puede crear y manipular una base de datos de prueba.
2. Puede ejecutar el flujo completo de backup.
3. Puede verificar/restaurar backups.
4. Puede detectar errores y corrupción.
5. Puede rotar correctamente los backups.
6. Puede recuperar correctamente después de una interrupción.
7. Puede comprobar el comportamiento de `state.json`.
8. Puede comprobar `pending_sync`.
9. Puede probar los backends sin tocar datos reales.
10. Puede ejecutar pruebas repetibles desde una máquina de desarrollo o CI.
11. Puede utilizar SQLite para tests rápidos.
12. Puede utilizar SQL Server aislado para pruebas de integración que necesiten específicamente comportamiento de SQL Server.

 La infraestructura de pruebas debe permitir comprobar que los servicios funcionan correctamente **antes de confiarles los backups reales**.

---

 # 1\. Objetivo principal

 Implementar dos niveles de persistencia para testing:

```
                 ┌──────────────────────────────┐
                 │       TEST SUITE             │
                 └──────────────┬───────────────┘
                                │
                ┌───────────────┴────────────────┐
                │                                │
         SQLite Test DB                    SQL Server Test DB
         rápida / aislada                  integración real
                │                                │
                ▼                                ▼
       datos seed conocidos             datos seed conocidos
                │                                │
                └───────────────┬────────────────┘
                                │
                                ▼
                       Application Layer
                                │
                ┌───────────────┼────────────────┐
                ▼               ▼                ▼
             Local             R2             Server
            Backend          Backend          Backend
                │
                ▼
          C:\BackupsTest\
```

 **IMPORTANTE:**

 `C:\Backups\` continúa siendo exclusivamente la ubicación de backups reales.

 Todos los tests deben utilizar una raíz independiente:

```
C:\BackupsTest\
```

 o una ruta temporal equivalente proporcionada por el test.

 **Nunca utilizar `C:\Backups\` durante los tests.**

---

 # 2\. Regla absoluta de aislamiento

 Los tests jamás pueden conectarse a:

```
Caproba01\vbadilla
```

 ni a:

```
CONTABILIDAD
```

 de producción.

 La infraestructura de testing debe impedirlo incluso por configuración accidental.

 Agregar una validación de seguridad que rechace explícitamente configuraciones de test que intenten utilizar:

```
CONTABILIDAD
Caproba01\vbadilla
C:\Backups\
```

 cuando el modo de ejecución sea `test`.

 Ejemplo conceptual:

```
type TestEnvironmentConfig struct {
    DatabaseDriver string
    DatabaseName   string
    BackupRoot     string
    TestMode       bool
}
```

 Si:

```
TestMode == true
```

 entonces la infraestructura debe validar que:

 - La base sea una base de pruebas.
- La ruta de backups sea una ruta de pruebas.
- No se utilice la instancia/base de producción.
- No se utilice la carpeta real de backups.

 El test debe fallar inmediatamente si detecta una configuración insegura.

---

 # 3\. Arquitectura de testing

 Agregar una estructura equivalente a:

```
femucaribe-backup-agent/
├── cmd/
│   └── backup-agent/
│
├── internal/
│   ├── application/
│   ├── cli/
│   ├── config/
│   ├── logging/
│   ├── rotation/
│   ├── state/
│   ├── storage/
│   │   ├── local/
│   │   ├── r2/
│   │   └── server/
│   │
│   └── ui/
│
├── test/
│   ├── fixtures/
│   │   ├── seed/
│   │   ├── expected/
│   │   └── corrupt/
│   │
│   ├── integration/
│   │   ├── sqlite/
│   │   ├── sqlserver/
│   │   ├── backup/
│   │   ├── restore/
│   │   ├── rotation/
│   │   └── recovery/
│   │
│   ├── helpers/
│   │   ├── filesystem/
│   │   ├── database/
│   │   ├── process/
│   │   └── assertions/
│   │
│   └── seeds/
│       ├── sqlite/
│       └── sqlserver/
│
├── scripts/
│   ├── test-sqlserver.ps1
│   ├── test-sqlite.ps1
│   ├── seed-sqlserver.ps1
│   └── cleanup-test.ps1
│
├── docker/
│   └── sqlserver/
│       └── docker-compose.yml
│
└── README.md
```

 La estructura exacta puede adaptarse al proyecto existente, pero debe mantenerse claramente separada la infraestructura de producción de la infraestructura de testing.

---

 # 4\. SQLite para tests rápidos

 Agregar soporte de SQLite específicamente para tests.

 El objetivo de SQLite NO es reemplazar SQL Server en producción.

 SQLite existe únicamente para:

 - tests unitarios/integración rápidos;
- pruebas de repositorios;
- pruebas de transacciones;
- pruebas de seed;
- pruebas de estado;
- pruebas de backup lógico o flujo abstracto;
- pruebas de rotación;
- pruebas de corrupción;
- pruebas de recuperación;
- pruebas repetitivas;
- tests que no necesiten características específicas de SQL Server.

 Seleccionar un driver SQLite adecuado para Go y fijar explícitamente la dependencia en `go.mod`.

 Antes de elegirlo, verificar compatibilidad con:

 - Windows;
- `go test`;
- CI;
- SQLite en memoria;
- SQLite sobre archivo;
- concurrencia suficiente para los tests;
- mantenimiento activo.

 No agregar una dependencia innecesaria si el proyecto ya tiene una alternativa compatible.

---

 # 5\. Test database factory

 Crear una abstracción para crear bases de datos de prueba.

 Ejemplo conceptual:

```
type TestDatabase interface {
    Driver() string
    DSN() string
    Seed(ctx context.Context) error
    Reset(ctx context.Context) error
    Close() error
}
```

 Y un factory:

```
type DatabaseFactory interface {
    SQLite(ctx context.Context) (TestDatabase, error)
    SQLServer(ctx context.Context) (TestDatabase, error)
}
```

 La implementación debe permitir que un test solicite:

```
db := factory.SQLite(ctx)
```

 o:

```
db := factory.SQLServer(ctx)
```

 sin que el test tenga que conocer detalles de conexión.

---

 # 6\. SQLite en memoria y SQLite en archivo

 Soportar dos modalidades.

 ## SQLite memory

 Para tests ultrarrápidos:

```
:memory:
```

 Debe utilizarse para pruebas donde no sea necesario conservar la base entre procesos.

 ## SQLite file

 Para pruebas que necesiten:

 - interrupción del proceso;
- archivos reales;
- corrupción;
- copias;
- backup físico del archivo;
- reapertura posterior;
- comportamiento después de una interrupción.

 Ejemplo:

```
C:\BackupsTest\db\test-contabilidad.sqlite
```

 Los nombres deben generarse de manera aislada para que tests paralelos no compartan accidentalmente la misma base.

---

 # 7\. Seed de datos

 Crear un sistema formal de seed.

 No quiero datos hardcodeados dispersos dentro de cada test.

 Los datos deben vivir en fixtures reproducibles.

 Por ejemplo:

```
test/
└── fixtures/
    └── seed/
        ├── customers.sql
        ├── accounts.sql
        ├── invoices.sql
        └── transactions.sql
```

 o el formato que mejor se adapte al proyecto.

 Debe existir un seed mínimo y determinístico que permita comprobar:

 - inserción;
- actualización;
- eliminación;
- relaciones;
- registros suficientes para detectar pérdida de datos;
- datos con caracteres especiales;
- números;
- fechas;
- valores NULL;
- textos largos;
- múltiples registros.

 El seed debe poder ejecutarse repetidamente de manera controlada.

 Debe existir una función equivalente a:

```
SeedTestData(ctx, db)
```

 y una forma de limpiar/reinicializar:

```
ResetTestData(ctx, db)
```

---

 # 8\. Dataset de prueba conocido

 Definir un dataset pequeño pero suficientemente representativo.

 Por ejemplo:

```
CUSTOMERS
- 10 registros

ACCOUNTS
- 20 registros

INVOICES
- 50 registros

TRANSACTIONS
- 200 registros
```

 Los números exactos son negociables.

 Lo importante es que exista una cantidad conocida y verificable.

 Después del seed debe poder hacerse:

```
AssertSeedIntegrity(ctx, db)
```

 y verificar:

 - cantidad de registros;
- claves esperadas;
- relaciones;
- valores críticos.

 Esto permitirá detectar si un backup/restauración perdió registros.

---

 # 9\. SQL Server aislado

 Para pruebas que necesiten SQL Server real, crear una instancia aislada mediante Docker.

 Debe utilizarse una imagen de SQL Server soportada y compatible con las restricciones del proyecto.

 La base de pruebas debe tener un nombre explícitamente diferente de producción, por ejemplo:

```
CONTABILIDAD_TEST
```

 o:

```
FEMUCARIBE_BACKUP_TEST
```

 Nunca:

```
CONTABILIDAD
```

 La instancia de test debe ser completamente independiente de:

```
Caproba01\vbadilla
```

---

 # 10\. Docker Compose para SQL Server

 Agregar:

```
docker/sqlserver/docker-compose.yml
```

 Debe permitir:

```
docker compose -f docker/sqlserver/docker-compose.yml up -d
```

 y:

```
docker compose -f docker/sqlserver/docker-compose.yml down
```

 La configuración debe estar orientada exclusivamente a desarrollo/testing.

 No incluir credenciales reales del sistema.

 Usar credenciales claramente ficticias y específicas del entorno de pruebas.

 Documentar que nunca deben reemplazarse por credenciales de producción dentro del archivo.

---

 # 11\. SQL Server 2012

 Existe una consideración importante:

 El agente productivo debe seguir siendo compatible con:

```
SQL Server 2012
```

 pero la infraestructura Docker de testing **no debe asumir automáticamente que una imagen moderna de SQL Server reproduce exactamente todos los comportamientos de SQL Server 2012**.

 Por lo tanto:

 - mantener compatibilidad del código con SQL Server 2012;
- utilizar SQL Server aislado para pruebas de integración;
- documentar claramente qué pruebas son de compatibilidad específica con SQL Server;
- si se requiere validación estricta contra SQL Server 2012, permitir configurar externamente un servidor SQL Server 2012 de laboratorio;
- nunca utilizar producción para este propósito.

 El test runner debe permitir seleccionar:

```
SQLite
SQL Server Docker
SQL Server externo de laboratorio
```

 sin modificar el código de negocio.

---

 # 12\. Configuración del entorno de tests

 Crear una configuración específica para testing.

 Ejemplo:

```
TEST_DATABASE_DRIVER=sqlite
TEST_DATABASE_PATH=...
TEST_BACKUP_ROOT=C:\BackupsTest\
```

 Para SQL Server:

```
TEST_DATABASE_DRIVER=sqlserver
TEST_SQLSERVER_HOST=localhost
TEST_SQLSERVER_PORT=...
TEST_SQLSERVER_DATABASE=CONTABILIDAD_TEST
TEST_SQLSERVER_USER=...
TEST_SQLSERVER_PASSWORD=...
```

 No utilizar secretos reales.

 Preferir variables de entorno o archivos locales ignorados por Git.

 Agregar:

```
.env.test.example
```

 si resulta útil.

 Nunca agregar:

```
.env.test
```

 con credenciales reales al repositorio.

---

 # 13\. Backup directory de testing

 Toda ejecución de integración debe tener una raíz aislada.

 Por defecto:

```
C:\BackupsTest\
```

 Pero los tests deberían preferentemente crear subdirectorios temporales:

```
C:\BackupsTest\
    run-001\
    run-002\
```

 o usar `t.TempDir()` cuando sea posible.

 Ejemplo:

```
C:\BackupsTest\integration-abc123\
```

 Al terminar correctamente, el test debe limpiar sus archivos.

 Si un test falla, debe existir una opción para conservar los artefactos para diagnóstico.

 Por ejemplo:

```
KEEP_TEST_ARTIFACTS=true
```

---

 # 14\. Test del flujo completo

 Crear un test de integración que ejecute conceptualmente:

```
crear DB
   ↓
seed
   ↓
verificar seed
   ↓
ejecutar backup
   ↓
verificar archivo
   ↓
SHA-256
   ↓
RESTORE VERIFYONLY
   ↓
restaurar a DB temporal
   ↓
comparar registros
   ↓
OK
```

 La prueba debe demostrar que los datos restaurados son equivalentes a los datos originales.

 No alcanza con comprobar:

```
archivo existe
```

 Debe comprobarse el contenido.

---

 # 15\. Verificación de registros

 Después de restaurar un backup de prueba:

```
DB original
     │
     ├── seed
     │
     ▼
 backup
     │
     ▼
 restore
     │
     ▼
DB restaurada
```

 Comparar:

 - cantidad de tablas;
- cantidad de registros;
- claves primarias;
- registros críticos;
- checksums lógicos cuando sea razonable;
- relaciones importantes.

 Crear helpers reutilizables:

```
AssertDatabaseEquivalent(
    ctx,
    original,
    restored,
)
```

 No depender exclusivamente de `COUNT(*)`.

 Para datos importantes, verificar valores concretos.

---

 # 16\. Prueba de corrupción

 Crear tests que simulen corrupción del backup.

 Flujo:

```
crear backup válido
       ↓
modificar bytes del .bak
       ↓
ejecutar VERIFYONLY
       ↓
debe fallar
```

 El sistema debe:

 - detectar el archivo inválido;
- no marcarlo como backup válido;
- no actualizar `state.json` como éxito;
- no ejecutar rotación destructiva basada en ese archivo;
- dejar un log claro.

---

 # 17\. Prueba de archivo temporal

 Verificar:

```
CONTABILIDAD_YYYYMMDD_HHMM.bak.tmp
```

 durante el proceso.

 Nunca debe aparecer como backup final válido.

 Si el proceso se interrumpe:

```
.bak.tmp
```

 debe quedar disponible para que la siguiente ejecución lo detecte y elimine.

 Crear un test específico:

```
run 1
 ↓
interrupción
 ↓
.tmp existe
 ↓
run 2
 ↓
.tmp eliminado
 ↓
nuevo backup
```

---

 # 18\. Prueba de recuperación después de kill

 Crear una prueba controlada que permita interrumpir el proceso durante el backup.

 No utilizar un `kill` arbitrario que vuelva el test flaky.

 Agregar un mecanismo de test-only para introducir un punto de interrupción conocido.

 Por ejemplo:

```
TEST_FAILPOINT=after_backup_started
```

 o una abstracción equivalente.

 IMPORTANTE:

 Los failpoints de test **no deben estar activos en producción**.

 El objetivo es probar:

```
backup iniciado
       ↓
proceso interrumpido
       ↓
.tmp
       ↓
segunda ejecución
       ↓
limpieza
       ↓
backup válido
```

---

 # 19\. Prueba de rotación

 Usar:

```
C:\BackupsTest\
```

 y crear backups simulados:

```
backup-01.bak
backup-02.bak
backup-03.bak
backup-04.bak
backup-05.bak
```

 Verificar que:

```
keep = 3
```

 deja exactamente las tres copias correctas.

 También comprobar:

```
.tmp
```

 no cuenta como backup válido.

 Casos mínimos:

 - 0 archivos;
- 1 archivo;
- N-1;
- N;
- N+1;
- múltiples `.tmp`;
- archivos corruptos;
- nombres inválidos;
- fechas iguales;
- archivos con timestamps inesperados.

---

 # 20\. Prueba de state.json

 El entorno de test debe comprobar:

```
backup exitoso
      ↓
state.json
      ↓
last_run_date
last_backup_file
sha256
```

 Y también:

```
backup fallido
      ↓
state.json
```

 debe conservar el estado anterior y no marcar una ejecución fallida como exitosa.

 Probar:

 - archivo inexistente;
- JSON corrupto;
- campos faltantes;
- escritura interrumpida;
- lectura concurrente;
- recuperación.

---

 # 21\. Prueba de pending\_sync

 Crear pruebas que simulen:

```
backup local OK
      ↓
R2 falla
      ↓
pending_sync.r2 = true
```

 Después:

```
segunda ejecución
      ↓
detecta pending_sync
      ↓
intenta sincronizar primero
      ↓
si OK
      ↓
pending_sync.r2 = false
```

 Esta prueba debe ejecutarse con un backend R2 mock/fake o MinIO, nunca contra producción.

---

 # 22\. Prueba de múltiples backends

 El test debe verificar que:

```
LocalBackend
R2Backend
ServerBackend
```

 pueden coexistir sin que un backend conozca internamente a los demás.

 Especialmente:

```
Backend
   ↓
error
   ↓
application
   ↓
RetryableError
   ↓
state repository
```

 Debe verificarse que ningún backend modifica directamente:

```
state.json
```

---

 # 23\. Backups de prueba

 Los archivos generados por tests deben tener nombres inequívocos.

 Ejemplo:

```
CONTABILIDAD_TEST_20260910_1015.bak
```

 Nunca:

```
CONTABILIDAD_20260910_1015.bak
```

 cuando el archivo pertenece a una prueba.

 Esto evita confusiones humanas al inspeccionar el directorio.

---

 # 24\. No reutilizar configuración de producción

 No permitir que los tests carguen accidentalmente:

```
config.json
config.dat
state.json
```

 de producción.

 El test runner debe construir explícitamente sus dependencias:

```
application.NewApp(testConfig, testState, testBackends, ...)
```

 con objetos aislados.

 No usar variables globales.

 No usar rutas relativas ambiguas que dependan del directorio desde el cual se ejecutó `go test`.

---

 # 25. Test de seguridad de configuración

 Agregar un test específico que intente configurar:

```
database = CONTABILIDAD
server = Caproba01\vbadilla
backup = C:\Backups\
TestMode = true
```

 y comprobar que:

```
config.Validate()
```

 rechaza la configuración.

 Este test es obligatorio.

 Su finalidad es evitar que una modificación futura elimine accidentalmente la protección contra producción.

---

 # 26\. Diferenciar SQLite de SQL Server

 No fingir que SQLite es SQL Server.

 Los tests deben estar etiquetados.

 Por ejemplo:

```
Unit
SQLite
SQLServer
R2
E2E
```

 SQLite puede validar:

 - repositorios;
- estado;
- seed;
- transacciones;
- lógica de aplicación;
- comparación de registros;
- recuperación;
- aislamiento.

 SQL Server debe utilizarse para validar específicamente:

 - conexión real;
- `BACKUP DATABASE`;
- `RESTORE VERIFYONLY`;
- comportamiento SQL Server;
- permisos;
- tamaños;
- errores propios del motor;
- restauración.

 Nunca sustituir una prueba específica de SQL Server por SQLite simplemente porque sea más rápida.

---

 # 27\. Tags para tests

 Permitir ejecutar:

```
go test ./...
```

 para la suite rápida.

 Y separar las pruebas que necesitan infraestructura externa.

 Por ejemplo:

```
go test -tags=integration ./test/...
```

 o una estrategia equivalente.

 Debe existir una separación clara entre:

```
go test ./...
```

 y:

```
tests que necesitan Docker/SQL Server/MinIO
```

 El comando rápido no debe fallar porque Docker no está iniciado.

---

 # 28\. Comandos esperados

 Documentar comandos similares a:

```
go test ./...
```

```
go test -tags=integration ./...
```

```
docker compose -f docker/sqlserver/docker-compose.yml up -d
```

```
go test -tags=sqlserver ./...
```

```
docker compose -f docker/sqlserver/docker-compose.yml down
```

 Los comandos exactos pueden adaptarse a la implementación final.

---

 # 29\. Script de preparación

 Crear:

```
scripts/test-sqlserver.ps1
```

 que pueda:

 1. comprobar Docker;
2. levantar SQL Server;
3. esperar hasta que esté disponible;
4. crear la base de pruebas;
5. ejecutar seed;
6. ejecutar los tests;
7. mostrar el resultado;
8. opcionalmente limpiar el entorno.

 Debe manejar correctamente timeout si SQL Server no llega a estar disponible.

 No asumir que el contenedor está listo inmediatamente después de:

```
docker compose up
```

---

 # 30\. Script de limpieza

 Crear:

```
scripts/cleanup-test.ps1
```

 Debe poder eliminar:

```
C:\BackupsTest\
```

 y cualquier infraestructura Docker exclusivamente de testing.

 IMPORTANTE:

 El script debe tener protecciones para impedir que una ruta vacía o incorrecta termine apuntando a:

```
C:\Backups\
```

 No ejecutar comandos destructivos sobre una ruta que no haya sido validada.

---

 # 31\. Artefactos de diagnóstico

 Cuando un test de integración falle, permitir conservar:

```
backup .bak
state.json
logs
database file
docker logs
```

 en:

```
test-artifacts/
```

 o una ubicación temporal equivalente.

 Esto debe permitir investigar:

```
¿Por qué falló el backup?
¿Por qué VERIFYONLY falló?
¿Qué registros se perdieron?
¿Qué quedó en state.json?
¿Qué ocurrió antes del kill?
```

---

 # 32\. Prueba de comparación antes/después

 Crear una utilidad que genere un snapshot lógico de la base:

```
type DatabaseSnapshot struct {
    Tables map[string]TableSnapshot
}

type TableSnapshot struct {
    RowCount int
    Hash     string
}
```

 El hash debe generarse de forma determinística.

 Por ejemplo:

```
ordenar registros
      ↓
serialización estable
      ↓
SHA-256
```

 Así:

```
snapshot(original) == snapshot(restored)
```

 significa que el contenido probado es equivalente.

 No utilizar orden natural de SQL sin `ORDER BY`, porque puede no ser determinista.

---

 # 33\. Test end-to-end principal

 Debe existir un test principal que represente el escenario real:

```
1. Crear entorno limpio.
2. Crear DB de prueba.
3. Ejecutar seed.
4. Validar seed.
5. Ejecutar backup.
6. Verificar archivo temporal/final.
7. Ejecutar VERIFYONLY.
8. Calcular SHA-256.
9. Restaurar backup.
10. Comparar snapshot original/restaurado.
11. Verificar state.json.
12. Ejecutar rotación.
13. Verificar cantidad de backups.
14. Simular un nuevo backup.
15. Verificar que la copia anterior no se elimina antes de validar la nueva.
16. Simular fallo.
17. Verificar que el estado permanece consistente.
18. Limpiar.
```

---

 # 34\. Test de prevención de pérdida de datos

 Este es uno de los objetivos más importantes de esta fase.

 Crear un escenario:

```
Backup A válido
       ↓
Backup B comienza
       ↓
Backup B falla
       ↓
Backup A sigue intacto
       ↓
state.json sigue apuntando a A
```

 Y otro:

```
Backup A válido
       ↓
Backup B válido
       ↓
VERIFYONLY OK
       ↓
Backup B pasa a final
       ↓
rotación
       ↓
A puede eliminarse solamente si corresponde
```

 Nunca:

```
borrar A
   ↓
intentar crear B
   ↓
B falla
   ↓
NO BACKUP
```

 La prueba debe proteger explícitamente contra ese escenario.

---

 # 35\. Test de SHA-256

 Además de los tests unitarios existentes, verificar en integración:

```
backup generado
      ↓
SHA-256 calculado
      ↓
leer archivo nuevamente
      ↓
SHA-256 idéntico
```

 Si el archivo cambia después de calcular el hash:

```
hash esperado != hash actual
```

 y el sistema debe detectarlo donde corresponda.

---

 # 36\. Compatibilidad Windows

 La infraestructura debe funcionar correctamente en Windows.

 Probar especialmente:

 - rutas `C:\...`;
- separadores de rutas;
- archivos bloqueados;
- renombrado atómico;
- eliminación;
- permisos;
- procesos;
- PID;
- `Get-Content -Wait`;
- Docker Desktop.

 No asumir comportamiento Unix para operaciones específicas de filesystem.

---

 # 37\. Tests paralelos

 Los tests deben ser seguros para ejecutar con:

```
go test ./... -count=1
```

 y, cuando corresponda:

```
go test ./... -parallel N
```

 Nunca compartir accidentalmente:

```
state.json
```

 ni:

```
C:\BackupsTest\backup.bak
```

 entre tests.

 Cada test debe recibir su propio:

```
temp directory
state
database
configuration
logger
```

 cuando corresponda.

---

 # 38\. Repetibilidad

 Un test exitoso debe seguir siendo exitoso si se ejecuta:

```
1 vez
10 veces
100 veces
```

 No depender de:

 - fecha actual;
- orden de ejecución;
- archivos existentes;
- estado residual;
- Docker previamente preparado;
- backups anteriores.

 Utilizar `t.TempDir()` y fixtures determinísticos siempre que sea posible.

---

 # 39\. Qué NO hacer

 NO:

 - tocar `CONTABILIDAD` de producción;
- tocar `Caproba01\vbadilla`;
- utilizar `C:\Backups\` para tests;
- utilizar credenciales reales;
- subir backups de tests al R2 real;
- ejecutar pruebas destructivas contra producción;
- modificar la lógica de negocio solamente para facilitar tests;
- convertir SQLite en requisito de producción;
- asumir que SQLite reproduce exactamente SQL Server;
- eliminar tests existentes;
- reemplazar tests SQL Server específicos por SQLite;
- introducir mocks donde una prueba real sea necesaria;
- hacer que los tests dependan del orden de ejecución;
- usar datos aleatorios sin seed reproducible;
- guardar secretos de testing reales en Git.

---

 # 40\. Compatibilidad con la arquitectura existente

 Esta fase no debe romper:

```
internal/application/
internal/cli/
internal/storage/
internal/state/
internal/ui/
```

 La infraestructura de tests debe consumir las mismas interfaces que utiliza producción.

 Si para poder testear es necesario agregar una interfaz, factory o dependency injection, hacerlo en el punto correcto.

 No crear caminos paralelos que hagan que:

```
test → código especial
```

 mientras:

```
producción → código real
```

 si el objetivo de la prueba es validar comportamiento real.

 La prueba debe utilizar el mismo pipeline de aplicación que utilizará producción.

---

 # 41\. Condición de aceptación principal

 La fase se considera exitosa únicamente cuando sea posible ejecutar:

```
go test ./...
```

 para la suite rápida y obtener:

```
PASS
```

 sin necesitar SQL Server ni Docker.

 Y adicionalmente:

```
go test -tags=integration ./...
```

 con la infraestructura correspondiente levantada, ejecutando las pruebas de integración.

 Debe existir además una suite específica para SQL Server que demuestre:

```
seed
 ↓
backup
 ↓
VERIFYONLY
 ↓
restore
 ↓
comparación de registros
```

 sin utilizar producción.

---

 # 42\. Condiciones de aceptación de seguridad

 Debe existir un test automatizado que demuestre:

```
TEST MODE + CONTABILIDAD producción
             ↓
          RECHAZADO
```

 y:

```
TEST MODE + C:\Backups\
             ↓
          RECHAZADO
```

 Mientras que:

```
TEST MODE + CONTABILIDAD_TEST
             +
C:\BackupsTest\
             ↓
          ACEPTADO
```

 Este test es obligatorio y debe permanecer en la suite para evitar regresiones.

---

 # 43\. Condición de aceptación de backup

 Debe demostrarse automáticamente que:

```
backup válido
```

 puede ser:

```
creado
→ verificado
→ restaurado
→ comparado
```

 sin pérdida de registros.

 Debe existir al menos un test que falle si se pierde un solo registro crítico del dataset de prueba.

---

 # 44\. Condición de aceptación de recuperación

 Debe demostrarse:

```
proceso interrumpido
       ↓
.tmp huérfano
       ↓
siguiente ejecución
       ↓
limpieza
       ↓
nuevo backup válido
```

 sin:

 - duplicación incorrecta;
- corrupción;
- pérdida del backup válido anterior;
- `state.json` inconsistente.

---

 # 45\. Condición de aceptación de rotación

 Debe demostrarse:

```
keep = 3
```

 mantiene exactamente las tres copias válidas más recientes.

 Un `.tmp` nunca debe contar como una copia válida.

 Una copia nueva solo puede entrar a rotación después de:

```
BACKUP
+
VERIFYONLY
+
rename final
+
SHA-256
```

 según el pipeline existente.

---

 # 46\. Condición de aceptación de ServerBackend futuro

 La infraestructura de testing debe quedar preparada para que cuando se implemente:

```
ServerBackend
```

 se puedan agregar tests sin modificar:

```
Cobra
TUI
application contract
```

 innecesariamente.

 La prueba debe tratar los backends mediante la interfaz:

```
Backend
```

 cuando corresponda.

---

 # 47\. README

 Actualizar `README.md` con una sección completa:

```
Testing
```

 que explique:

 - diferencia entre tests unitarios;
- SQLite;
- SQL Server;
- Docker;
- seed;
- backups de prueba;
- `C:\BackupsTest\`;
- cómo ejecutar tests rápidos;
- cómo ejecutar integración;
- cómo levantar SQL Server;
- cómo ejecutar seed;
- cómo limpiar;
- cómo conservar artefactos;
- cómo ejecutar tests repetidamente;
- cómo verificar que nunca se utiliza producción.

 Incluir una advertencia visible:

```
⚠️ TESTING SAFETY

Los tests nunca deben ejecutarse contra:
Caproba01\vbadilla
CONTABILIDAD
C:\Backups\
```

---

 # 48\. Qué quiero como entregable de este bloque

 ## Bloque A — Infraestructura base

 1. Factory de bases de datos de prueba.
2. SQLite en memoria.
3. SQLite en archivo.
4. Seed determinístico.
5. Reset/cleanup.
6. Fixtures.
7. Helpers de comparación.
8. Configuración aislada de tests.
9. Protección contra producción.

 **STOP. Esperar revisión.**

---

 ## Bloque B — SQL Server aislado

 1. Docker Compose para SQL Server.
2. Script PowerShell de preparación.
3. Health check/wait.
4. Creación de DB de prueba.
5. Seed SQL Server.
6. Cleanup.
7. Documentación.

 **STOP. Esperar revisión.**

---

 ## Bloque C — Integración del backup

 1. Test SQLite del flujo de aplicación.
2. Test SQL Server real del flujo de backup.
3. `BACKUP DATABASE`.
4. `RESTORE VERIFYONLY`.
5. Restore.
6. Comparación de datos.
7. SHA-256.
8. `state.json`.

 **STOP. Esperar revisión.**

---

 ## Bloque D — Fallos y recuperación

 1. `.tmp` huérfano.
2. Kill controlado.
3. Backup corrupto.
4. `VERIFYONLY` fallido.
5. Fallo de disco simulado.
6. Fallo de estado.
7. Recuperación posterior.
8. Verificación de que nunca se pierde la copia válida anterior.

 **STOP. Esperar revisión.**

---

 ## Bloque E — Backends y regresión

 1. Tests de LocalBackend.
2. Tests de R2Backend mediante mock/MinIO.
3. Tests de `pending_sync`.
4. Verificación de rotación.
5. Verificación de aislamiento.
6. Confirmar que ningún backend modifica `state.json`.
7. Regresión completa de las fases anteriores.

 **STOP. Esperar revisión.**

---

 # 49\. Regla final de implementación

 Esta fase existe para aumentar la **confianza y prevención de pérdida de datos**, no para agregar características visibles al usuario.

 La prioridad es:

```
SEGURIDAD
    ↓
REPRODUCIBILIDAD
    ↓
AISLAMIENTO
    ↓
DETECCIÓN DE CORRUPCIÓN
    ↓
VERIFICACIÓN DE RESTORE
    ↓
DETECCIÓN DE PÉRDIDA DE REGISTROS
    ↓
RECUPERACIÓN
    ↓
VELOCIDAD DE TEST
```

 SQLite debe utilizarse para que la mayoría de las pruebas sean extremadamente rápidas.

 SQL Server aislado debe utilizarse para comprobar que las operaciones específicas de SQL Server realmente funcionan.

 **Nunca utilizar la base de producción como entorno de prueba.**

 No modificar `CONTABILIDAD` ni ninguna configuración de producción para hacer pasar un test.

 No hacer commits.

 Mostrar el diff completo de cada archivo antes de aplicar cualquier cambio.

 Después de cada bloque indicado arriba, hacer **STOP DURO** y esperar explícitamente mi revisión.

 **No continuar automáticamente al siguiente bloque.**

 ### Una mejora importante que incorporé

 Hay una distinción que creo que conviene mantener muy clara: **SQLite no debería intentar simular `BACKUP DATABASE` de SQL Server**. SQLite sirve para probar muy rápido la capa de aplicación, repositorios, seeds, estados, aislamiento, recuperación y lógica general. Pero las cosas específicas de SQL Server —`BACKUP DATABASE`, `RESTORE VERIFYONLY`, restore real, permisos, errores del motor— deben probarse contra **SQL Server aislado**.

 Así evitás tener una falsa sensación de seguridad del tipo _"todos los tests pasan en SQLite, entonces el backup SQL Server funciona"_.

 También agregué la protección automatizada contra `Caproba01\vbadilla`, `CONTABILIDAD` y `C:\Backups\`. Para este proyecto me parece especialmente importante: **el propio test suite debe impedir que un error de configuración termine apuntando a producción**.