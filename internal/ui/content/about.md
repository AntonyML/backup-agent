# BACKUP AGENT ENTERPRISE

Agente de backups autónomo y tolerante a fallos para bases de datos SQL Server empresariales.

## Arquitectura
- **Motor:** Go (Arquitectura Limpia / Hexagonal)
- **CLI & TUI:** Cobra + Bubble Tea v2 + Lip Gloss v2 + Glamour v2
- **Seguridad:** Cifrado con Windows DPAPI (Data Protection API) en `config.dat`
- **Almacenamiento:** Pipeline multi-backend (Local, Supabase Storage, Cloudflare R2, Servidor UNC)
- **Integridad:** `RESTORE VERIFYONLY` en SQL Server y hash SHA-256 por streaming
