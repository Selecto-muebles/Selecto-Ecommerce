# Runtime serverless

## Modos del contenedor

La misma imagen soporta estos modos:

```text
selecto-ecommerce
selecto-ecommerce job expire-orders
selecto-ecommerce job email-outbox
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

## Despliegue

Cloud Run Service ejecuta el modo API. Dos Cloud Run Jobs reutilizan la misma
imagen y reemplazan sus argumentos. Cloud Scheduler los invoca con OIDC. La
frecuencia se define en infraestructura y no dentro del proceso.

Las reclamaciones usan `FOR UPDATE SKIP LOCKED`, los lotes son idempotentes y
los claims abandonados se recuperan mediante los mecanismos existentes.

