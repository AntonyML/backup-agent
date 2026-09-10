# FEMUCARIBE Backup Agent

Agente de backups autónomo y tolerante a fallos diseñado para la base de datos SQL Server `CONTABILIDAD`.

## Arquitectura
- **Motor:** Go (Arquitectura Limpia / Hexagonal)
- **CLI & TUI:** Cobra + Bubble Tea v2 + Lip Gloss v2 + Glamour v2
- **Seguridad:** Cifrado con Windows DPAPI (Data Protection API) en `config.dat`
- **Almacenamiento:** Pipeline multi-backend (Local, Cloudflare R2, Servidor Fase 3)
- **Integridad:** `RESTORE VERIFYONLY` en SQL Server y hash SHA-256 por streaming
