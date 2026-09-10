# Configuración y Credenciales

El agente utiliza dos archivos de configuración principales:

## 1. `config.json` (Parámetros locales)
- `backup_dir`: Directorio donde SQL Server escribe los backups locales (debe ser local a SQL Server).
- `server`: Instancia de SQL Server (ej: `localhost` o `SERVER\INSTANCIA`).
- `database`: Nombre de la base de datos (`CONTABILIDAD`).
- `retain`: Cantidad de copias locales a conservar (por defecto `3`).
- `login_timeout_sec`: Timeout para conectar a SQL Server.
- `backup_timeout_sec`: Timeout máximo para la operación de backup.

## 2. `config.dat` (Credenciales seguras de Cloudflare R2)
- Cifrado mediante **Windows DPAPI** (`CryptProtectData`) bajo la identidad del usuario actual (`CURRENT_USER`).
- Almacena: `Endpoint`, `Bucket`, `Access Key ID` y `Secret Access Key`.
- Se puede configurar interactivamente desde la pantalla de Configuración `[C]` o con `backup-agent configure`.
