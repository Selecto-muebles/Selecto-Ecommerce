# Tandas de despacho de Selecto

## Regla de corte

- Hora fija: 13:00, `America/Argentina/Buenos_Aires`, lunes a sábado.
- El domingo no tiene corte. Sus compras se incluyen en el corte del lunes a las 13:00.
- Cada orden pagada con dirección, DNI, teléfono, email y artículos completos entra en una sola tanda. Si se completan datos después de un corte, entra en el siguiente.
- Preparar una tanda crea un CSV persistente, su SHA-256 y la relación única con cada orden. Descargarla varias veces devuelve los mismos bytes. No cambia pagos, pedidos ni envíos.
- El primer CSV con órdenes puede contener pedidos históricos. Antes de compartirlo, cotejar los números de orden con los archivos ya enviados; el sistema no conoce esos envíos manuales previos. Las órdenes excluidas por datos incompletos se cuentan y se revisan en «Preparación de despachos».

## Operación en Admin

1. Ir a **Órdenes → Tandas por corte** y actualizar. El panel muestra el próximo corte y las tandas preparadas.
2. Si el job de GCP aún no está habilitado o falló, pulsar **Preparar cortes vencidos**. Esta operación es idempotente; no adelanta el corte.
3. Revisar cantidad de órdenes, renglones y excluidas; en la primera tanda, cotejar contra envíos anteriores.
4. Descargar el CSV de la tanda. Enviarlo a Gestionar por el canal privado previamente acordado, tras verificar el destinatario. La descarga no lo envía automáticamente. Si se configura `GESTIONAR_WHATSAPP_DESTINATION` en el backend con el número internacional sin `+`, **Avisar por WhatsApp** abre el chat con un recordatorio sin datos personales; el operador debe confirmar el envío.
5. Solo después de enviarlo, registrar canal, destinatario y referencia en **Registrar entrega a logística**. Este registro audita la acción del operador; no demuestra recepción del proveedor. Volver a descargar nunca reenvía ni cambia el estado.
6. Conservar el acuse de recepción por fuera del sistema hasta disponer de una integración contractual verificable.

## Activación programada en GCP (requiere aprobación manual)

El código incluye `job logistics-cutoff`, pero el despliegue no crea ni programa recursos. Crear un Cloud Run Job `destry-logistics-cutoff-staging` con la misma imagen, cuenta de servicio, conexión VPC, secreto `DATABASE_URL` y `DB_SCHEMA=commerce` que `destry-expire-orders-staging`, cambiando solo los argumentos por `job logistics-cutoff`. Crear un Cloud Scheduler Job en `southamerica-east1` con cron `0 13 * * 1-6`, zona `America/Argentina/Buenos_Aires`, cuenta de servicio `destry-scheduler@destry-development.iam.gserviceaccount.com` y destino Cloud Run Jobs API para ejecutar el nuevo job. Verificar identidad, imagen y permisos antes de activar. El workflow de Ecommerce actualizará la imagen del nuevo job en futuros despliegues solo si ya existe.

Antes de programarlo, ejecutar manualmente una vez en una ventana controlada y revisar el primer CSV histórico. La ejecución del job es repetible; si falla, Admin permite recuperar los cortes vencidos. Para revertir la automatización, pausar el Scheduler. Las tandas ya guardadas permanecen disponibles para auditoría.

Costos de referencia: Cloud Scheduler cobra USD 0,10 por job cada 31 días después de tres jobs gratuitos por cuenta de facturación. En el proyecto había tres jobs al revisar; otras cargas de la misma cuenta pueden cambiar el costo incremental. Cloud Run Jobs cobra por recursos usados durante cada ejecución. Obtener la duración y el consumo reales tras la primera ejecución para cuantificarlo. No se modifica Cloud SQL ni se crea una réplica con este cambio.
