# Ciclo de Ejecución de Backup

El pipeline de backup sigue un orden estricto para garantizar que nunca se dañen los datos existentes:

1. **Exclusión Mutua:** Se adquiere `agent.lock` con PID para prevenir ejecuciones concurrentes.
2. **Limpieza de Huérfanos:** Se eliminan archivos temporales `.tmp` residuales de caídas previas.
3. **Sincronización Previa:** Si existe una copia pendiente para Cloudflare R2, se reintenta antes de generar una nueva.
4. **Idempotencia Diaria:** Solo se ejecuta una vez al día automáticamente, a menos que se fuerce con `[B]` o `--force`.
5. **Estimación y Espacio en Disco:** Se verifica el espacio requerido antes de escribir.
6. **Backup y Verificación:** Se realiza el `BACKUP DATABASE` hacia un temporal, se valida con `RESTORE VERIFYONLY` y se calcula el SHA-256 antes del renombrado atómico a `.bak`.
7. **Rotación Local:** Se conservan las 3 copias más recientes.
8. **Subida a R2:** Se sube al bucket S3 y se valida el tamaño remoto exacto.
