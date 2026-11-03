# OpenTelemetry de Ecommerce en Cloud Trace

## Alcance

Ecommerce emite trazas de solicitudes HTTP con nombres de ruta acotados y estado HTTP. El muestreo es del 1 %. No se agregan URL completas, cuerpos, encabezados, identificadores de cliente ni datos de pago a los spans. Las métricas operativas de Cloud Run y las métricas comerciales de Admin son independientes; este cambio no crea métricas OpenTelemetry ni instrumenta Payments.

## Destino y permisos

- Endpoint: `https://telemetry.googleapis.com/v1/traces` (OTLP/HTTP).
- Proyecto: `destry-development`, indicado mediante `OTEL_GCP_PROJECT_ID`.
- Identidad: `destry-ecommerce-runtime@destry-development.iam.gserviceaccount.com`, mediante credenciales predeterminadas de Cloud Run. No se configura token estático.
- Permisos: `roles/telemetry.tracesWriter` y `roles/serviceusage.serviceUsageConsumer` sobre `destry-development`.
- APIs necesarias: Telemetry, Cloud Trace, Cloud Logging y Cloud Monitoring; todas estaban habilitadas al revisar.

## Activación y verificación

Activado el 3 de noviembre de 2026 en `destry-ecommerce-staging` (`southamerica-east1`) con `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=https://telemetry.googleapis.com/v1/traces` y `OTEL_GCP_PROJECT_ID=destry-development`. El código obtiene tokens renovables de la cuenta de servicio. La revisión `destry-ecommerce-staging-00060-wfb` estaba lista y servía el 100 % del tráfico; imagen, identidad, VPC, límites y demás variables se conservaron.

Cloud Run usa **CPU asignada durante toda la vida de la instancia** (`run.googleapis.com/cpu-throttling=false`). Fue necesario para que el exportador por lotes funcionara después de terminar una solicitud. Esto cambia el servicio a facturación por instancia mientras exista una instancia, aunque puede escalar a cero. Revisar el costo real en Billing; la cuenta de facturación comparte la franquicia de Cloud Run con otros proyectos.

Consultar Admin → Observabilidad: debe mostrar `Configurada`, `1 %` y un enlace a Cloud Trace. Ese estado solo confirma la configuración. La recepción se verificó por la API de Cloud Trace: una solicitud `GET /categories` con identificador de prueba produjo un span llamado `GET /categories` en el proyecto `destry-development`. Para nuevos diagnósticos, buscar `service.name=selecto-ecommerce` en Trace Explorer. Una traza ausente tras una única solicitud puede deberse al muestreo; si faltan trazas de forma sostenida, revisar IAM, logs del exportador y la asignación de CPU.

Para revertir, eliminar las dos variables `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` y `OTEL_GCP_PROJECT_ID` del servicio, volver a CPU asignada solo durante solicitudes (`--cpu-throttling`) y verificar una revisión lista. El código deja el tracing desactivado cuando no hay endpoint. Los permisos IAM pueden retirarse después de verificar que ningún otro servicio los utiliza.

Cloud Trace incluye 2,5 millones de spans por mes sin cargo por cuenta de facturación y cobra por spans ingeridos luego de ese umbral. Revisar el uso real; el 1 % de muestreo limita el volumen.
