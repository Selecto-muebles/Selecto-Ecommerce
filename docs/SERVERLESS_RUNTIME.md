# Runtime serverless

## Modos del contenedor

La misma imagen soporta estos modos:

```text
selecto-ecommerce
selecto-ecommerce job expire-orders
selecto-ecommerce job email-outbox
selecto-ecommerce job dispatch-batches
```

El primer comando inicia solamente la API HTTP cuando `APP_ENV=production`.
Los otros comandos procesan un lote acotado y finalizan.

## Configuracion

- `RUN_EMBEDDED_WORKERS=true` conserva los tickers durante desarrollo local.
- En produccion el valor debe ser `false`; la API rechaza otra configuracion.
- `JOB_TIMEOUT` limita cada ejecucion y admite como maximo una hora.
- `RELEASE_WORKER_MAX_BATCHES` limita el drenaje de ordenes expiradas.
- `EMAIL_WORKER_BATCH_SIZE` limita los correos reclamados por ejecucion.

El job `expire-orders` requiere solamente PostgreSQL y configuracion de
reservas. El job `email-outbox` requiere PostgreSQL y SMTP. Ninguno inicia el
servidor HTTP.

`dispatch-batches` requiere PostgreSQL y la migracion `025`. Asigna una orden
pagada una sola vez a una tanda de corte de lunes a sabado a las 13:00 de
Argentina (`DISPATCH_CUTOFF_HOUR`, valor por defecto 13). Los domingos y los
sabados despues del corte pasan al lunes. Debe ejecutarse periodicamente
(por ejemplo, despues de cada corte y durante el dia para conciliaciones
tardias); configurar Cloud Scheduler queda pendiente de aprobacion separada.
Admin puede preparar tandas vencidas manualmente mientras tanto. No genera
envios, no modifica pedidos ni contacta a Gestionar.

La primera descarga del CSV sella la tanda: las siguientes descargas mantienen
los mismos pedidos. Un pago confirmado tarde pasa al siguiente corte no sellado
y vencido. El CSV contiene datos personales; se descarga desde Admin autenticado
con `Cache-Control: no-store`. WhatsApp solo abre un borrador sin esos datos;
el operador adjunta el archivo y verifica destinatario y recepcion. El numero
`GESTIONAR_WHATSAPP_RECIPIENT` se configura en el backend en formato internacional
sin `+`; ningun envio automatico ni confirmacion de recepcion estan habilitados.

## Despliegue

Cloud Run Service ejecuta el modo API. Dos Cloud Run Jobs reutilizan la misma
imagen y reemplazan sus argumentos. Cloud Scheduler los invoca con OIDC. La
frecuencia se define en infraestructura y no dentro del proceso.

Las reclamaciones usan `FOR UPDATE SKIP LOCKED`, los lotes son idempotentes y
los claims abandonados se recuperan mediante los mecanismos existentes.

