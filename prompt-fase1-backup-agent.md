# Prompt — Fase 1: Backup Agent FEMUCARIBE (Go)

IMPORTANTE: no hagas commits sin que yo diga explícitamente "commit autorizado". Mostrame el diff completo de cada archivo antes de aplicar cualquier cambio. Al terminar cada bloque de trabajo, hacé un stop duro y esperá mi revisión antes de seguir con el siguiente.

## Contexto

Vamos a construir un agente de backups para la base de datos SQL Server `CONTABILIDAD` (sistema SIDC de FEMUCARIBE, instancia `Caproba01\vbadilla`), que corre como proceso batch de vida corta disparado por Windows Task Scheduler — NO es un demonio ni un servicio persistente.

Esta es la **Fase 1 únicamente**: backup local + rotación. NO implementes todavía subida a Cloudflare R2, ni copia a servidor, ni reporte a Supabase. Esas son fases posteriores y quiero revisarlas por separado.

## Alcance de esta fase

1. Ejecutar `BACKUP DATABASE CONTABILIDAD` contra la instancia SQL Server usando **Windows Integrated Authentication** (Trusted Connection) — NO usuario/contraseña SQL.
2. Escribir el backup primero como archivo temporal (`.bak.tmp`), correr `RESTORE VERIFYONLY` sobre ese temporal, y solo si es válido renombrarlo (rename atómico) al nombre final `CONTABILIDAD_YYYYMMDD_HHMM.bak`.
3. Calcular SHA-256 del `.bak` final y guardarlo junto al archivo (o en el `state.json`).
4. Rotación local: mantener solo las **3 copias más recientes** en `C:\Backups\` (o la ruta configurada), borrando las más viejas — solo después de confirmar que la nueva copia es válida.
5. Estado persistente en `state.json` junto al binario, con al menos: `last_run_date`, `last_backup_file`, `sha256`.
6. Lock file (con PID) para evitar corridas simultáneas. Si el PID del lock ya no existe, tratarlo como huérfano y limpiarlo.
7. Al iniciar cada corrida: si hay algún `.tmp` huérfano de una corrida anterior interrumpida, borrarlo antes de empezar.
8. Idempotencia diaria: si `last_run_date` == hoy, no repetir el backup (salir con log informativo).
9. Log a archivo rotativo por día (`logs/agent-YYYY-MM-DD.log`), soportando `tail -f` estilo PowerShell (`Get-Content -Wait`).
10. CLI mínima: el binario debe poder correr sin argumentos (modo normal, para Task Scheduler) y aceptar un flag o subcomando para forzar corrida manual (para soporte).

## Estructura de proyecto esperada

```
femucaribe-backup-agent/
├── cmd/backup-agent/main.go
├── internal/
│   ├── config/       # carga y validación de configuración
│   ├── state/         # lectura/escritura de state.json
│   ├── lock/           # manejo de lock file
│   ├── sqlbackup/     # ejecución de BACKUP DATABASE / RESTORE VERIFYONLY
│   ├── hasher/        # SHA-256
│   ├── rotation/       # lógica de rotación (N copias más recientes)
│   └── logger/         # logging a archivo rotativo
├── go.mod
├── go.sum
└── README.md
```

## Decisiones técnicas ya tomadas (no las cuestiones, ya se evaluaron alternativas)

- Lenguaje: Go, binario único, sin frameworks pesados.
- Driver SQL: `github.com/microsoft/go-mssqldb` (driver nativo), no `sqlcmd.exe` por subproceso — necesitamos manejo de errores tipado y timeouts vía `context`.
- Autenticación SQL: Windows Integrated Auth (`Trusted_Connection=yes` en el DSN), no credenciales SQL en texto plano.
- Nada de config.yaml en texto plano — la config de esta fase no necesita secretos todavía (R2/Supabase vienen en fases posteriores, ahí se resuelve con DPAPI). Por ahora la config puede ser un `config.json` simple con: ruta de backups, nombre de instancia/base, cantidad de copias a retener (default 3).
- No implementar reintentos de red en esta fase (no hay red involucrada todavía).
- No implementar soft delete ni "nunca borrar" — es rotación simple con borrado físico de las copias más viejas.

## Casos de falla que el código debe manejar explícitamente

- SQL Server detenido / instancia inaccesible → falla rápido, log claro, no deja `state.json` ni archivos a medio escribir.
- Disco local sin espacio suficiente → chequear espacio libre disponible ANTES de arrancar `BACKUP DATABASE`, fallar rápido con mensaje claro si no alcanza.
- Proceso interrumpido (kill/apagón) a mitad del `BACKUP DATABASE` → el `.tmp` queda huérfano, se descarta en la siguiente corrida, no se intenta "resumir".
- Dos disparos el mismo día (startup + trigger diario) → lock file + `last_run_date` evitan duplicar trabajo.
- `state.json` corrupto o ausente en el primer arranque → tratar como "nunca se corrió", no crashear.

## Tests que quiero incluidos en esta fase

Unitarios (sin SQL Server real):
- Rotación: dado un listado de archivos con distintas fechas, verificar cuáles borra (casos borde: menos de N, exactamente N, con `.tmp` mezclados).
- Lock file: detectar lock huérfano (PID muerto) vs. lock legítimo (PID vivo).
- Lectura/escritura de `state.json`, incluyendo archivo corrupto o inexistente.
- Cálculo de SHA-256 contra un archivo de prueba con hash conocido.

Integración (requiere SQL Server real o contenedor Docker con SQL Server, NUNCA contra `CONTABILIDAD` de producción):
- `BACKUP DATABASE` + `RESTORE VERIFYONLY` contra una base de prueba pequeña.
- Simular kill a mitad del proceso (matar el proceso en un punto conocido) y verificar que la siguiente corrida recupera correctamente sin duplicar ni corromper nada.

## Qué quiero como entregable de este bloque

1. Estructura de carpetas y `go.mod` inicial.
2. Módulos `state`, `lock`, `rotation`, `hasher` con sus tests unitarios — completamente funcionales y testeados, sin tocar SQL Server todavía.
3. Un `README.md` explicando cómo correr los tests y cómo se estructura el proyecto.

Detené ahí y esperá mi revisión antes de tocar el módulo `sqlbackup` (que sí requiere SQL Server real para probarse).
