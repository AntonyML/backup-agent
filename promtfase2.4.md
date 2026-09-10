Prompt — Fase 2.4: Backup Agent FEMUCARIBE — Configuración de Testing Desacoplada

IMPORTANTE: no hagas commits sin que yo diga explícitamente "commit autorizado". Mostrame el diff completo de cada archivo antes de aplicar cualquier cambio. Al terminar el bloque de trabajo, hacé un STOP DURO y esperá mi revisión.

Contexto

En la Fase 2.3 se agregó la infraestructura de testing con SQLite y SQL Server aislado.

Actualmente algunos valores del entorno de producción están hardcodeados en las validaciones de seguridad de los tests, por ejemplo:

Caproba01\vbadilla
CONTABILIDAD
C:\Backups\


Esto debe corregirse.

Objetivo

Mantener esos valores como defaults para FEMUCARIBE, pero permitir configurar otros valores cuando el proyecto se ejecute en otra PC, servidor o entorno de laboratorio.

Por ejemplo:

TEST_PRODUCTION_SQLSERVER=Caproba01\vbadilla
TEST_PRODUCTION_DATABASE=CONTABILIDAD
TEST_PRODUCTION_BACKUP_PATH=C:\Backups\


Si no se configuran, utilizar esos valores como defaults.

La configuración debe permitir cambiar:

instancia SQL Server de producción;
nombre de la base de producción;
ruta de backups de producción;
instancia SQL Server de testing;
nombre de la base de testing;
ruta de backups de testing.
Regla de seguridad

La protección contra producción no debe eliminarse.

Debe cambiar de:

if server == "Caproba01\\vbadilla" {
    reject()
}


a algo conceptualmente equivalente a:

if server == cfg.ProductionSQLServer {
    reject()
}


donde cfg.ProductionSQLServer proviene de configuración y tiene como valor por defecto:

Caproba01\vbadilla


Lo mismo para:

CONTABILIDAD
C:\Backups\

Configuración

Crear una única estructura de configuración para el entorno de testing, por ejemplo:

type TestEnvironmentConfig struct {
    ProductionSQLServer   string
    ProductionDatabase    string
    ProductionBackupPath  string

    TestSQLServer          string
    TestDatabase           string
    TestBackupPath         string
}


Los defaults deben ser:

ProductionSQLServer  = Caproba01\vbadilla
ProductionDatabase   = CONTABILIDAD
ProductionBackupPath = C:\Backups\

TestSQLServer        = localhost
TestDatabase         = CONTABILIDAD_TEST
TestBackupPath       = C:\BackupsTest\


Los valores exactos de testing también deben poder cambiarse.

Fuente de configuración

Preferir una solución simple y adecuada para tests:

variables de entorno;
archivo .env.test ignorado por Git;
o configuración equivalente ya existente en el proyecto.

No duplicar sistemas de configuración innecesariamente.

Agregar un ejemplo:

.env.test.example


sin credenciales reales.

Validaciones

TestEnvironmentConfig.Validate() debe comprobar como mínimo:

Los valores obligatorios no estén vacíos.
TestDatabase no sea igual a ProductionDatabase.
TestBackupPath no sea igual a ProductionBackupPath.
El entorno de test no utilice accidentalmente la instancia de producción.
Las rutas sean válidas.
No permitir que el test runner utilice producción por una configuración accidental.

La comparación debe ser robusta frente a diferencias de mayúsculas/minúsculas y rutas equivalentes cuando corresponda.

Tests

Agregar tests para:

usar todos los defaults;
sobrescribir solamente ProductionSQLServer;
sobrescribir solamente ProductionDatabase;
sobrescribir rutas;
configurar una PC diferente;
rechazar test apuntando a producción;
aceptar una configuración de laboratorio válida;
verificar que C:\BackupsTest\ no sea confundido con C:\Backups\;
verificar que la configuración no dependa de valores hardcodeados fuera del sistema de defaults.
Importante

No modificar la lógica de backup, SQLite, SQL Server, R2, TUI, Cobra ni application.

Esta fase es únicamente un refactor de configuración del entorno de testing.

No eliminar las protecciones de seguridad.

No introducir credenciales reales.

No hacer commits.

Mostrar el diff completo de cada archivo antes de aplicar cambios.

Al terminar, hacer STOP DURO y esperar mi revisión.