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

Con el sandbox configurado en servidor y sin exponer secretos al navegador:
crear un paquete sintético, verificar un único ID externo persistido, repetir
la misma solicitud para demostrar ausencia de duplicados, solicitar un retiro
de prueba, consultar su estado y probar errores/timeout. Recién después de
eso se implementa y habilita el envío automático con un cambio de configuración
separado. No reutilizar los datos de producción para pruebas.

Después de merge y despliegue manual del backend, el gateway requiere su
activación manual habitual; verificar sus cuatro rutas con
`SELECTO_API_URL=https://api.selectosport.com node scripts/smoke_gestionar_gateway.mjs`.
