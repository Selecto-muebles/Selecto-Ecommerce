# Gestionar: preparación operativa de Selecto

## Qué queda listo sin acceso al proveedor

- El backoffice puede consultar órdenes **pagadas** que aún están en `preparing` o
  `ready_for_dispatch`. Incluye órdenes atrasadas; no se limita al día actual.
- La vista previa usa `order_shipping_addresses`, la dirección congelada en la
  compra, y no el perfil actual del cliente. Nombre de producto y SKU también
  quedan congelados en `order_items` para nuevas compras; los pedidos anteriores
  sólo pueden tomar el valor del catálogo al aplicar la migración 021.
- Se señalan dirección, contacto, artículos y SKU faltantes. `data_ready` sólo
  significa que los datos internos están completos: **no autoriza un POST**.
- Las rutas son administrativas y de solo lectura. No crean paquetes, retiros
  ni estados externos. `provider_submission` permanece en
  `disabled_pending_contract`.
- La migración 022 agrega un registro durable, todavía sin activar: una clave
  única por pedido para paquetes, lotes de retiro sin pedidos duplicados y
  eventos de estado externo deduplicados. Reservar no equivale a enviar. Sólo
  las pruebas ejercitan esas transiciones; ninguna ruta HTTP las invoca.
- Un intento que pudiera haber llegado al proveedor queda `uncertain` tras un
  timeout. No se reenvía automáticamente: primero hay que consultar a
  Gestionar si creó el paquete o el retiro y asociar su ID externo.
- El seguimiento manual existente sigue disponible para registrar información
  **confirmada** por logística; no se debe inventar un número de guía.

## Qué se puede certificar ahora, sin acceso a Gestionar

- Aplicar las migraciones en PostgreSQL aislado, repetirlas y auditar el esquema.
  Probar creación de orden, snapshot de dirección y SKU, bandeja y vista previa,
  incluidos datos faltantes y cambios posteriores del catálogo.
- Ejecutar `go test -race ./...` con `TEST_DATABASE_URL` local. Las pruebas del
  cliente de lectura usan un servidor TLS simulado y verifican token sólo en el
  servidor, límites de respuesta, errores y rechazo de redirecciones.
- El E2E del admin usa respuestas simuladas: prueba la UI, no la aceptación de
  paquetes ni retiros por Gestionar. Un selector de Postman con nombre
  `Gestionar External Api` no convierte `localhost` o `{{base_url}}` en sandbox.
- No usar el token publicado en ejemplos para pruebas ni considerarlo secreto
  de producción. Sin URL real y contrato confirmado no hay prueba externa.
- La migración 022 y su auditoría se probaron en PostgreSQL aislado; el schema
  de producción requiere la activación manual habitual. El gateway también
  necesita una API Config nueva: desplegar Cloud Run no habilita sus rutas.

## Qué muestran las dos variantes del importador

- El ejemplo JSON de `POST /api/external-client/package/not-linked-register`
  envuelve los envíos en `packages[]`. Cada paquete muestra
  `destinatario`, `direccion`, `localidad`, `codigo_postal`, `telefono`,
  `correo`, `tipo_de_entrega`, `fecha_de_venta`, `turbo`, `monto_a_pagar`,
  `observaciones`, `sku`, `cantidad` y `nombre`. El sobre usa
  `type: "fullfilment"` y `condition: "cambio"` (se conserva la grafía del
  ejemplo). Esto **no demuestra** que esos valores correspondan a una venta
  normal ya pagada en Selecto.
- La otra variante presentada bajo «Import masivo de paquetes no vinculados»
  usa `multipart/form-data` con `file` y `condition`. No equivale a enviar el
  JSON anterior ni demuestra que el archivo sea obligatorio para Selecto.
- Los valores de ejemplo para fecha, código postal, `turbo` y
  `monto_a_pagar` no son un contrato validado. Tampoco se proporcionó una
  respuesta exitosa de creación de paquetes. Por eso el adaptador de escritura
  sigue desactivado y no se transforman pedidos reales a ninguno de los dos
  formatos por conjetura.

## Contrato que debe confirmar Gestionar

1. URL HTTPS de prueba y credenciales separadas de producción. No copiar el
   token que aparece en ejemplos públicos; rotarlo si tiene validez real.
2. Si la venta normal de Selecto entra por el JSON o por el importador de
   archivos de `POST /api/external-client/package/not-linked-register`, y qué
   `type` y `condition` corresponden a esa venta. Los ejemplos disponibles
   mezclan fulfillment y cambios; no asumir que sirven para ventas ordinarias.
3. Campos obligatorios, formato de fecha/código postal/teléfono, tratamiento
   de pedidos con varios artículos y significado de `monto_a_pagar` cuando el
   cliente **ya pagó online**. Nunca cobrar contra entrega sin confirmación.
4. ID externo por paquete y respuesta de error/éxito; estrategia documentada
   de idempotencia o consulta previa para evitar duplicados después de un
   timeout. Acordar si existe un ID de pedido de Selecto en el contrato.
5. `platform_id` de Selecto (los ejemplos usan valores de otros marketplaces),
   `pickup_time_id`, formato de solicitud de retiro y política para reintentos.
   El ejemplo de `POST /api/external-client/pickup` usa `orders_ids`,
   `platform_id` y `pickup_time_id`, pero sólo muestra un error 400 por paquete
   inexistente; no demuestra el alta exitosa de un retiro.
6. Endpoint o webhook para estados, firma/autenticación, catálogo de estados,
   paginación, cadencia de consulta y URL de seguimiento.

## Puerta para activar escrituras

Tras confirmar el contrato, falta conectar el registro transaccional a un
adaptador HTTP y a acciones administrativas protegidas. Debe transformar la
venta normal pagada al formato confirmado, crear un paquete sintético, guardar
el ID externo, reconciliar un timeout sin duplicar, pedir un retiro por lote,
consultar estados autenticados y mapearlos a `shipments` sin retrocesos. Sólo
entonces se habilita el envío con un cambio de configuración separado. No
reutilizar datos ni credenciales de producción para estas pruebas.

Después de merge y despliegue manual del backend, el gateway requiere su
activación manual habitual; verificar sus cuatro rutas con
`SELECTO_API_URL=https://api.selectosport.com node scripts/smoke_gestionar_gateway.mjs`.
