# Operación Admin y comunicaciones Selecto

## Alcance y límites

Estos cambios pertenecen a `Selecto-muebles`: Ecommerce, Admin y Storefront.
No reemplazan proyectos de la organización Destry, no alteran Mobbex y no crean
un CMS, otro servidor, una nueva base ni otra cola. Los merges son manuales.
La migración 017 es aditiva; conserva usuarios, sesiones, productos y pedidos.

## Operación administrativa

- Categorías: edición de nombre/slug/estado y orden, con auditoría transaccional.
  El ID y la relación con productos se conservan. Renombrar actualiza también la
  proyección de texto histórica. Desactivar oculta navegación, no elimina productos.
- Reseñas: páginas de 20, filtros, publicación/rechazo/retorno a revisión y nota
  interna. Cada decisión registra moderador, fecha y transición anterior/nueva.
  La nota nunca forma parte de la API pública. Compra verificada no es editable.
- KPI: períodos móviles de 7, 30 o 90 días y ventana anterior de igual duración;
  cobros, órdenes cobradas, ticket, unidades y clientes nuevos/recurrentes.
  La jornada comercial usa Argentina. Preparación/despacho muestra cantidad,
  demoras mayores a 24 horas y máxima antigüedad desde el cambio de estado del envío.
  Editar una nota no reinicia ese reloj. Sin costos o eventos de navegación no se
  inventan margen, conversión ni abandono de carrito.
- Imágenes al crear productos, galería, desactivar/activar y eliminar conservan
  sus flujos existentes, incluidos permisos y protección de historial de pedidos.

## Newsletter y Brevo

El consentimiento y su versión se guardan junto al evento de sincronización en
una transacción. Se reutilizan el worker privado de correos, Cloud Tasks y el job
de respaldo existentes. Las tareas son idempotentes; los fallos se reintentan con
espera creciente hasta ocho intentos. Admin muestra pendientes/fallos y permite
un reintento explícito auditado. Una tarea antigua no puede resuscribir después
de una baja: se serializan operaciones por contacto y se comprueba su versión.

La baja pública es `/newsletter/baja`: solicitar instrucciones no cambia estado
ni revela si existe el email. El enlace lleva un token aleatorio, guardado como
hash, válido 30 minutos y de un solo uso. El usuario confirma con POST; abrirlo
o escanearlo por GET no da de baja. Hay límite de una solicitud por minuto por
suscripción, además de los límites generales de peticiones.

Los webhooks registran una clave deduplicada, fecha del proveedor y estados de
entrega/rebote/supresión. No degradan una entrega por un evento anterior ni
aceptan escrituras sin Bearer válido. El Gateway conserva ese Bearer en
`X-Forwarded-Authorization`. Una supresión bloquea resuscripciones automáticas y
retira sólo la membresía de newsletter: no bloquea globalmente correos de compra.
No se elimina la blacklist de Brevo ni se reactiva consentimiento con un reintento.

SMTP incluye `X-Mailin-custom` con `event_key` para correlacionar callbacks de
orden, pago, envío y baja. Aceptado por SMTP **no significa entregado**. Admin lo
distingue y sólo muestra entrega confirmada cuando recibe el webhook correspondiente.

## Configuración previa a la activación

Por defecto `BREVO_CONTACTS_ENABLED=false`: SMTP sigue funcionando y los contactos
pendientes permanecen en outbox sin enviarse a la API de Brevo. No se debe declarar
certificada la sincronización real hasta completar lo siguiente:

1. Brevo → CRM → Contactos → Listas: elegir/crear **Newsletter Selecto** y obtener
   su ID numérico. No usar ID de contacto, carpeta o campaña.
2. Guardar una **API key**, diferente de la contraseña SMTP, en Secret Manager de
   `destry-development` con nombre `BREVO_API_KEY`. No pegarla en Git, capturas ni chat.
3. Guardar otro secreto aleatorio de al menos 32 caracteres como `BREVO_WEBHOOK_TOKEN`.
4. Identificar las cuentas de servicio de ejecución de los recursos existentes
   y darles `roles/secretmanager.secretAccessor` sólo sobre los secretos necesarios.
   No dar permisos de proyecto ni cambiar WIF para esta integración.
5. En API `destry-ecommerce-staging`, worker `destry-email-outbox-worker-staging` y
   job `destry-email-outbox-staging`, región `southamerica-east1`, configurar:
   `BREVO_CONTACTS_ENABLED=true`, `BREVO_LIST_ID=<ID>`,
   `BREVO_API_BASE_URL=https://api.brevo.com/v3`, `MARKETING_WORKER_BATCH_SIZE=20`
   y referencia `BREVO_API_KEY=BREVO_API_KEY:<version fijada>`.
   Sólo la API necesita la referencia al secreto `BREVO_WEBHOOK_TOKEN`.
   Usar `--update-env-vars`/`--update-secrets`, nunca sobrescribir el conjunto
   completo: conservar DB, JWT, SMTP, audiencias privadas, red y service accounts.
6. Crear/verificar dos webhooks de Brevo hacia
   `https://api.selectosport.com/communications/brevo/webhook`, con Bearer igual
   al secreto anterior y `batched=false`:
   - Transaccional: `request`, `delivered`, `hardBounce`, `softBounce`, `blocked`,
     `spam`, `invalid`, `deferred`, `unsubscribed`.
   - Marketing: `delivered`, `hardBounce`, `softBounce`, `spam`, `unsubscribed`.
   Comprobar los hooks existentes antes de crearlos para no duplicar envíos.

Referencias oficiales: [listas](https://developers.brevo.com/reference/get-lists),
[Contacts API](https://developers.brevo.com/reference/create-contact),
[crear webhook](https://developers.brevo.com/reference/create-webhook),
[autenticación](https://developers.brevo.com/docs/secured-webhooks).

## Orden de publicación y certificación real

1. Preflight verde y merge manual del Backend. Ejecutar su workflow de despliegue:
   aplica 017, audita y actualiza las imágenes exactas en los recursos existentes.
2. Crear/activar una API Config nueva desde `ops/api-gateway/openapi.json`, que
   contiene 67 rutas, sin eliminar la configuración anterior. Verificar OPTIONS,
   PATCH de categorías, confirmación de baja, webhooks y rutas administrativas.
3. Merge/despliegue de Admin y Storefront. Si el frontend se despliega por merge,
   mantenerlo después del Gateway para evitar rutas nuevas no disponibles.
4. Activar Brevo con los datos anteriores. Comprobar en Comunicaciones que la
   configuración es válida y que el backlog se procesa; una bandera habilitada
   por sí sola no prueba conectividad ni recepción.
5. Sesión normal Admin: categoría temporal sin productos → editar, reordenar,
   desactivar; revisar notas y paginación; comparar KPI con pedidos conocidos;
   verificar alta con imagen, desactivación reversible y bloqueo de eliminación
   cuando hay historial. No borrar productos/órdenes reales para probar.
6. Con email de prueba autorizado: suscribir y comprobar membresía en Brevo;
   solicitar baja, abrir enlace sin confirmar (debe seguir suscrito), confirmar,
   comprobar retiro en Brevo y rechazar reutilización del token. Repetir un
   webhook real para verificar que no duplica efectos. No enviar rebotes o spam
   ficticios sobre direcciones reales.
7. Pedidos de prueba controlados: verificar correos de orden/pago/envío,
   correlación por `event_key`, entrega y recepción. La prueba local SMTP no
   certifica llegada a Principal ni reputación del dominio.

Si hay una regresión, deshabilitar contacto sync en los tres runtimes, mantener
outbox para reintentar, recuperar imagen/API Config anteriores y conservar 017.
No revertir destructivamente tablas, consentimiento, auditoría o tokens consumidos.

## Certificación local

Se verifican migración/auditoría/repetición en PostgreSQL aislado con rol no
superusuario, transacciones/rollback y carreras alta-baja; callbacks duplicados y
fuera de orden; SMTP local de cuatro plantillas y callback antes del ack; API sin
credenciales, expiración/reutilización de tokens y reintentos agotados.
Admin/Storefront incluyen pruebas de contratos, componentes y navegador con red
externa bloqueada. No reemplazan las pruebas autenticadas de producción descritas.
