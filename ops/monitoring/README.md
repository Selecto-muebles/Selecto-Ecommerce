# Monitoreo operativo de Selecto

Proyecto existente: `destry-development`. Los servicios conservan sus nombres
históricos `destry-*`; no cambiar infraestructura, DNS, IAM ni capacidad para
instalar estas alertas. No afecta el proyecto independiente Destry/KubiGeek.

## Cobertura

Tres chequeos públicos HTTPS GET, cada 5 minutos desde Iowa, Bélgica y São Paulo:

| Dominio / ruta | Qué valida |
|---|---|
| `https://selectosport.com/` | HTTP 200, TLS y título de Selecto |
| `https://admin.selectosport.com/login` | HTTP 200, TLS y título de Admin |
| `https://api.selectosport.com/ready` | HTTP 200 y readiness, incluida conexión SQL |

No usan credenciales, no crean pedidos/correos ni prueban JavaScript o sesiones.
Si cambia el título HTML legítimamente, actualizar el matcher junto al cambio.

Siete políticas, todas dirigidas al canal email explícitamente elegido:

1. Caída/certificado Storefront.
2. Caída/certificado Admin.
3. Caída/certificado API.
4. Cinco respuestas HTTP 5xx en 5 minutos por servicio Cloud Run de Selecto.
5. Al menos una ejecución fallida de expiración, email-outbox o notificaciones de pagos.
6. Jobs sin éxito: muestras ausentes o ventanas con cero éxitos (20 minutos para
   expiración/pagos; 75 minutos para el respaldo de correo que corre cada 30).
7. Fallos de SMTP/outbox/encolado y errores de Scheduler. Máximo un aviso por
   30 minutos para esta política; los reintentos automáticos pueden recuperarlos.

Uptime requiere dos ubicaciones fallando durante cinco minutos. El aviso no es
instantáneo: intervienen intervalo, ventana, ingestión y entrega del canal.
El certificado avisa por debajo de 14 días. Las métricas de ausencia necesitan
una serie observada y no garantizan alertar si se elimina el recurso monitorizado.
Las alertas métricas se cierran al recuperarse; el auto-cierre de 7 días cubre
incidentes sin datos nuevos, no demuestra recuperación por sí solo.

## Plan y aplicación manual

Requiere Node 22+, gcloud autenticado y permisos existentes de Monitoring.
El script no concede IAM ni habilita APIs. Reutiliza un canal email habilitado,
no crea destinos nuevos ni incluye correos personales en el repositorio.

```sh
node scripts/gcp/configure_monitoring.mjs --email=CORREO_CONFIRMADO
node scripts/gcp/configure_monitoring.mjs --email=CORREO_CONFIRMADO --apply --receipt=/tmp/selecto-monitoring-APLICACION.json
```

Si gcloud no está en PATH, definir `GCLOUD_BIN` apuntando a su ejecutable.
`--apply` requiere un recibo nuevo; no sobrescribe otro recibo. Guardarlo fuera
de Git. Contiene IDs creados/actualizados y estado anterior para revisión.

Sólo se administran recursos con `managed_by=selecto-monitoring-v1` y la clave
`selecto_key` exacta. Un nombre ocupado por otro recurso o etiquetas duplicadas
aborta antes de modificar nada. La ejecución repetida conserva IDs; no repite
POST automáticamente si una respuesta se pierde. Ante error parcial, revisar el
recibo, generar el plan y reintentar: los recursos ya creados se descubren por
etiquetas. No hay borrado de recursos ajenos ni despliegue automático desde CI.

## Comprobación posterior

- Revisar 3 uptime checks y 7 políticas habilitadas en Cloud Monitoring.
- Esperar métricas `monitoring.googleapis.com/uptime_check/check_passed` verdaderas
  para cada ubicación/check, y confirmar el canal en todas las políticas.
- Validar `run.googleapis.com/job/completed_execution_count`, etiqueta
  `result=succeeded`, para los tres jobs y comprobar sus horarios en Scheduler.
- Verificar destinatario y recepción mediante una prueba controlada del canal.
  No detener producción ni inyectar errores en pedidos/SMTP para probar avisos.
  Configuración aceptada por la API no equivale a recepción de un correo.
- Revisar cambios de costos y errores tras las primeras 24 horas.

Pruebas locales del reconciliador, sin GCP:

```sh
node --test scripts/gcp/monitoring.test.mjs
```

## Respuesta a incidentes y reversión

1. Abrir el recurso/revisión o job identificado en el incidente y sus logs.
2. Confirmar si es caída, fallo transitorio, mantenimiento o despliegue reciente.
3. Para cambios recientes, seguir el procedimiento de rollback de la imagen
   conocida. No recrear DB, cambiar JWT o ejecutar migraciones improvisadas.
4. Para correo, inspeccionar outbox y reintentos. No reenviar pagos ni correos
   ciegamente: comprobar idempotencia y resultado anterior.
5. Para revertir sólo monitoreo, usar los IDs del recibo: deshabilitar las
   políticas y pausar los chequeos creados; si se actualizaron recursos previos,
   restaurar su estado registrado. Mantener el canal existente intacto.

## Costo y límites de certificación

A 3 chequeos × 3 ubicaciones × 12 ejecuciones/hora × 24 × 31 días se estiman
80.352 ejecuciones/mes. Al 22/09/2026, la tarifa oficial incluye 1 millón de
ejecuciones mensuales por proyecto. La página de precios pospone la tarifa de
alerting hasta no antes de septiembre de 2027; revisar cambios antes de esa fecha.
Las métricas nativas usadas no requieren un servidor Prometheus ni métricas
personalizadas. Las peticiones a Cloud Run y los logs pueden tener consumo
incremental: esto no es garantía de factura cero ni un presupuesto global.

Fuentes oficiales:

- [Precios](https://cloud.google.com/products/observability/pricing).
- [Configuración de uptime](https://docs.cloud.google.com/monitoring/api/ref_v3/rest/v3/projects.uptimeCheckConfigs).
- [Políticas](https://docs.cloud.google.com/monitoring/alerts/policies-in-json).

Este monitoreo no certifica compras completas, Mobbex productivo, recepción de
todos los correos, capacidad bajo stress ni restauración de backups.
