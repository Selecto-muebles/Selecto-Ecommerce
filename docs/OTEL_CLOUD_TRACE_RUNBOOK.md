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

Después de mergear y desplegar el PR de Ecommerce, actualizar **solo** el servicio `destry-ecommerce-staging` en `southamerica-east1` con `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=https://telemetry.googleapis.com/v1/traces` y `OTEL_GCP_PROJECT_ID=destry-development`. El código obtiene tokens renovables de la cuenta de servicio. Verificar que la nueva revisión esté lista y que el resto de la configuración del servicio permanezca igual.

Consultar Admin → Observabilidad: debe mostrar `Configurada`, `1 %` y un enlace a Cloud Trace. Ese estado solo confirma la configuración. Para confirmar recepción, buscar una traza nueva con `service.name=selecto-ecommerce` en Cloud Trace después de generar una solicitud HTTP de prueba. Revisar los logs del servicio por errores del exportador si no aparece. Una traza ausente tras una única solicitud puede deberse al muestreo.

Para revertir, eliminar las dos variables `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` y `OTEL_GCP_PROJECT_ID` del servicio y verificar una revisión lista. El código deja el tracing desactivado cuando no hay endpoint. Los permisos IAM pueden retirarse después de verificar que ningún otro servicio los utiliza.

Cloud Trace incluye 2,5 millones de spans por mes sin cargo por cuenta de facturación y cobra por spans ingeridos luego de ese umbral. Revisar el uso real; el 1 % de muestreo limita el volumen.
