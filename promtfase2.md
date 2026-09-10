# Prompt — Fase 2: Backup Agent FEMUCARIBE (Go) — Subida a Cloudflare R2

IMPORTANTE: no hagas commits sin que yo diga explícitamente "commit autorizado". Mostrame el diff completo de cada archivo antes de aplicar cualquier cambio. Al terminar cada bloque de trabajo, hacé un stop duro y esperá mi revisión antes de seguir con el siguiente.

## Contexto

Esta es la Fase 2 del backup agent de FEMUCARIBE. La Fase 1 (backup local a `C:\Backups\`, rotación de 3 copias, `state.json`, lock file, `RESTORE VERIFYONLY`) ya está implementada y testeada — no la reimplementes ni la modifiques salvo que sea estrictamente necesario para integrar esta fase.

En esta fase agregamos: subir el `.bak` ya generado y verificado a Cloudflare R2, manteniendo solo **1 copia** en el bucket (la más reciente).

## Alcance de esta fase

1. **Manejo de secretos con DPAPI** — este es el primer punto del proyecto donde hay credenciales reales (R2 Access Key + Secret Key). Nada de texto plano en disco:
   - Agregar un subcomando `agent configure` que pida interactivamente Access Key, Secret Key, endpoint de R2 y nombre del bucket.
   - Cifrar cada valor con DPAPI (`CryptProtectData` vía `golang.org/x/sys/windows` o una librería equivalente ya validada), scope `CURRENT_USER`.
   - Guardar los blobs cifrados en `config.dat` (no en `config.json`, no en variables de entorno).
   - En runtime, desencriptar solo en memoria, nunca escribir el secreto en texto plano a disco ni loguearlo.

2. **Cliente R2 (S3-compatible)**:
   - Usar `github.com/aws/aws-sdk-go-v2` con el endpoint custom de R2.
   - Subir el `.bak` como objeto con key `CONTABILIDAD/<nombre-archivo>.bak` (o la convención que ya usa el nombre local).
   - Confirmar la subida verificando tamaño del objeto subido contra el tamaño local antes de considerarla exitosa (S3 no expone SHA-256 nativo salvo ETag en casos simples — no asumas que ETag es siempre MD5, no lo uses como verificación de integridad si el upload es multipart).

3. **Rotación remota (mantener 1 sola copia)**:
   - Después de confirmar que el nuevo objeto subió correctamente, listar objetos existentes bajo el prefijo `CONTABILIDAD/` y borrar todos excepto el recién subido.
   - Igual que en la Fase 1: nunca borrar el objeto anterior antes de confirmar que el nuevo está sano.

4. **Estado persistente extendido**:
   - Ampliar `state.json` con un campo `pending_sync.r2` (bool) y `r2_last_synced_file`.
   - Si la subida falla (red caída, timeout, error de R2), marcar `pending_sync.r2 = true` y **no bloquear el resto del proceso** — el backup local de la Fase 1 ya se considera exitoso independientemente de si R2 sincronizó.
   - Al iniciar cada corrida, si `pending_sync.r2 == true`, intentar sincronizar el archivo pendiente ANTES de generar un backup nuevo del día.

5. **Timeouts explícitos**: la subida a R2 debe correr con un `context.WithTimeout` razonable (ej. 5-10 min según tamaño esperado del `.bak`) — no queremos que un problema de red cuelgue el proceso indefinidamente en una corrida automática nocturna.

## Qué NO hacer en esta fase

- No implementar todavía la copia a servidor (Fase 3).
- No implementar todavía el reporte a Supabase (Fase 4) — si querés loguear el resultado de la subida a R2, que sea solo al log local por ahora.
- No implementar lógica de reintento agresivo/backoff sofisticado — eso es Fase 5. Por ahora "queda pendiente y se reintenta en la próxima corrida diaria" es suficiente.

## Casos de falla que el código debe manejar explícitamente

- Credenciales R2 inválidas o no configuradas (`agent configure` nunca se corrió) → error claro al log, no crashear el proceso completo (el backup local ya se hizo, eso no se pierde).
- Internet caído durante la subida → timeout controlado, marcar `pending_sync.r2 = true`, seguir sin bloquear.
- PC apagada/interrumpida a mitad de la subida → como `PutObject` es atómico del lado de R2 (no queda objeto parcial visible), simplemente el objeto nuevo nunca se confirmó; `pending_sync.r2` sigue en `true` desde antes de intentar, se reintenta en la próxima corrida.
- Falla al borrar el objeto anterior después de subir el nuevo (permisos, timeout) → no es crítico, loguear advertencia; puede quedar más de 1 copia temporalmente hasta la siguiente corrida exitosa, pero nunca debe fallar el proceso completo por esto.

## Tests que quiero incluidos en esta fase

Unitarios (con cliente R2 mockeado o contra MinIO local, NUNCA contra el bucket real de producción):
- Rotación remota: dado un listado simulado de objetos, verificar que borra todos menos el más reciente confirmado.
- Cifrado/descifrado DPAPI: round-trip de un valor de prueba (cifrar, guardar, leer, descifrar, comparar).
- Lógica de `pending_sync`: simular fallo de subida y verificar que el estado queda marcado correctamente y que la siguiente corrida prioriza el pendiente sobre generar un backup nuevo.

Integración (contra MinIO local levantado en Docker, simulando R2):
- Subida real de un archivo de prueba, confirmar objeto en bucket, confirmar rotación deja solo 1.
- Simular corte de conexión a mitad de subida (matar el proceso o cortar la red) y verificar que la siguiente corrida completa la sincronización sin duplicar ni corromper el estado.

## Qué quiero como entregable de este bloque

1. Subcomando `agent configure` funcional con cifrado DPAPI.
2. Módulo `internal/storage/r2/` con subida, rotación y manejo de `pending_sync` — testeado contra MinIO local.
3. Integración con el flujo principal de la Fase 1 (después de verificar el backup local, intentar sync a R2).
4. `README.md` actualizado con instrucciones de cómo correr `agent configure` y cómo levantar MinIO local para tests.

Detené ahí y esperá mi revisión antes de tocar la Fase 3 (copia a servidor).