# Tandas de despacho de Selecto

## Regla de corte

- Hora fija: 13:00, `America/Argentina/Buenos_Aires`, lunes a sábado.
- El domingo no tiene corte. Sus compras se incluyen en el corte del lunes a las 13:00.
- Cada orden pagada con dirección, DNI, teléfono, email y artículos completos entra en una sola tanda. Si se completan datos después de un corte, entra en el siguiente.
- Preparar una tanda crea un CSV persistente, su SHA-256 y la relación única con cada orden. Descargarla varias veces devuelve los mismos bytes. No cambia pagos, pedidos ni envíos.
- El primer CSV con órdenes puede contener pedidos históricos. Antes de compartirlo, cotejar los números de orden con los archivos ya enviados; el sistema no conoce esos envíos manuales previos. Las órdenes excluidas por datos incompletos se cuentan y se revisan en «Preparación de despachos».

## Operación en Admin

1. Ir a **Órdenes → Tandas por corte** y actualizar. El panel muestra el próximo corte y las tandas preparadas.
2. Si el job de GCP falló o no produjo la tanda esperada, pulsar **Preparar cortes vencidos**. Esta operación es idempotente; no adelanta el corte.
3. Revisar cantidad de órdenes, renglones y excluidas; en la primera tanda, cotejar contra envíos anteriores.
4. Descargar el CSV de la tanda. Enviarlo a Gestionar por el canal privado previamente acordado, tras verificar el destinatario. La descarga no lo envía automáticamente. Si se configura `GESTIONAR_WHATSAPP_DESTINATION` en el backend con el número internacional sin `+`, **Avisar por WhatsApp** abre el chat con un recordatorio sin datos personales; el operador debe confirmar el envío.
5. Solo después de enviarlo, registrar canal, destinatario y referencia en **Registrar entrega a logística**. Este registro audita la acción del operador; no demuestra recepción del proveedor. Volver a descargar nunca reenvía ni cambia el estado.
6. Conservar el acuse de recepción por fuera del sistema hasta disponer de una integración contractual verificable.

## Programación activa en GCP

Activado el 3 de noviembre de 2026 en `destry-development` / `southamerica-east1`:

- Cloud Run Job `destry-logistics-cutoff-staging`: imagen publicada de Ecommerce, argumentos `job logistics-cutoff`, cuenta `destry-ecommerce-runtime@destry-development.iam.gserviceaccount.com`, VPC privada, secreto `destry-ecommerce-database-url` y `DB_SCHEMA=commerce`.
- Cloud Scheduler Job del mismo nombre: cron `0 13 * * 1-6`, zona `America/Argentina/Buenos_Aires`, cuenta `destry-scheduler@destry-development.iam.gserviceaccount.com`, autorización `roles/run.invoker` sobre el job.
- Verificación: la primera ejecución manual preparó una tanda; la segunda preparó cero. Una ejecución disparada por Scheduler también terminó correctamente y preparó cero. El próximo corte programado al activar era el 4 de noviembre a las 13:00 de Buenos Aires.
- El workflow de Ecommerce actualizará la imagen del job en despliegues posteriores. Verificar el resultado del primer corte programado en **Órdenes → Tandas por corte** y en los logs del job.

La primera tanda debe revisarse contra los envíos manuales previos antes de compartir su CSV. Si falla un corte, Admin permite recuperar los vencidos. Para detener el horario, pausar el Scheduler; las tandas guardadas permanecen disponibles para auditoría.

Costos de referencia: Cloud Scheduler cobra USD 0,10 por job cada 31 días después de tres jobs gratuitos por cuenta de facturación. En el proyecto había tres jobs antes de crear este cuarto; otras cargas de la misma cuenta pueden cambiar el costo incremental. Cloud Run Jobs cobra por recursos usados durante cada ejecución. No se modificó Cloud SQL ni se creó una réplica con este cambio.
