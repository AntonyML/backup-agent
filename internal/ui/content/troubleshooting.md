# Diagnóstico y Resolución de Problemas

## ¿Qué significa que R2 esté en estado "PENDING" (Pending Sync)?

El estado **PENDING** en el backend de Cloudflare R2 ocurre cuando:
1. El backup local en SQL Server se realizó con total éxito, pasó la verificación de integridad `RESTORE VERIFYONLY`, se calculó su SHA-256 y quedó guardado de forma segura en disco local.
2. Sin embargo, la subida remota hacia Cloudflare R2 falló (por ejemplo: timeout de red, microcorte de internet o credenciales incorrectas).

### ¿Qué hacer?
- **El backup local NO se pierde ni se corrompe.** Está intacto y resguardado.
- Podés forzar la sincronización remota inmediata desde la TUI presionando la tecla **`[Y]` (Sync)**, o por línea de comandos ejecutando `backup-agent sync`.
- En la próxima ejecución automática de Task Scheduler, el agente detectará el pendiente y lo subirá a R2 **antes** de generar el backup del día.

---

## Error de Conexión a SQL Server
- Verificá que el servicio de SQL Server esté iniciado.
- Si usás Windows Authentication, asegurate de que el proceso corra bajo una cuenta con permisos `sysadmin` o `db_backupoperator` en la base de datos.

---

## Bloqueo por Instancia Activa (Lock Error)
- Si el proceso anterior terminó abruptamente (ej: corte de luz), el agente detecta automáticamente si el PID grabado en `agent.lock` ya no existe y reclama el lock.
- Si una instancia real continúa ejecutándose, esperá a que termine para evitar corrupción de copias.
