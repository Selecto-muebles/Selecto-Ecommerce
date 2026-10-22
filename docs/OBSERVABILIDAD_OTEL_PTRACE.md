# Observabilidad de Ecommerce: OpenTelemetry y diagnóstico puntual

## Alcance y estado

La API ya registra solicitudes con `request_id`, `correlation_id`, ruta y latencia en logs estructurados. El endpoint protegido `/admin/observability` muestra el estado de base de datos, reservas vencidas y eventos recientes del webhook. Ninguno de esos datos equivale hoy a una traza distribuida o a métricas globales de toda la plataforma.

La instrumentación de producción debe basarse en OpenTelemetry. Un `trace_id` permite seguir una operación entre Ecommerce y Payments, pero nunca debe usarse para marcar una orden como pagada: la fuente de verdad sigue siendo el estado confirmado por los servicios y la base de datos.

## Contrato de seguridad

- Registrar solo nombres de ruta parametrizados (por ejemplo `/orders/:id`), método, código HTTP, duración y nombre de servicio. No registrar URL completa, query string, cuerpo, cookies, tokens, direcciones de entrega ni datos de tarjeta.
- Mantener los IDs de solicitud y correlación existentes. Si hay un span válido, añadir `trace_id` y `span_id` a los logs, sin sustituir los IDs actuales.
- Usar un exportador OTLP configurable; sin endpoint configurado, la API debe seguir sirviendo compras normalmente y Admin debe indicar que la exportación está desactivada. Una caída del colector nunca debe impedir registrar órdenes o pagos.
- Evitar dimensiones de alta cardinalidad en métricas: no usar usuario, email, orden, pago, IP ni producto como etiquetas. Para análisis comercial por producto se necesita una canalización agregada y separada.
- Aplicar muestreo explícito, cuotas y retención antes de activar la exportación en producción. Verificar la versión del SDK/exportador con `govulncheck`; no introducir versiones anteriores a la corrección de GO-2026-6505.
- Distinguir en Admin métricas por instancia de métricas agregadas. Los contadores en memoria se reinician con cada despliegue y no deben mostrarse como totales globales.

## `ptrace` / `strace`: procedimiento excepcional

`ptrace` no es un sistema de KPI ni un sustituto de OpenTelemetry. En Cloud Run, la guía de Google para trazar llamadas al sistema exige coordinación con soporte antes de hacerlo. No habilitar capacidades de depuración, ejecutar `strace -f` de forma permanente ni adjuntarse a procesos de producción como parte de este despliegue.

Para una reproducción local o en un entorno aislado autorizado, usar un proceso de prueba sin secretos reales y preferir un resumen de llamadas (`strace -f -c`) durante un período corto. No capturar argumentos ni payloads de red, no subir volcados a tickets públicos y eliminar los artefactos diagnósticos según la política de retención acordada. El operador debe documentar motivo, alcance, duración y resultado.

## Puertas de aprobación antes de cambios en GCP

1. Inventariar en modo solo lectura servicios, región, volumen actual de logs/trazas, alertas y presupuesto.
2. Proponer el destino de OTLP (Cloud Trace vía colector u otro), credenciales/IAM mínimas, muestreo, retención, alertas y plan de reversión.
3. Presentar al dueño una estimación mensual con supuestos y costos marginales de Cloud Trace, Logging, colector/Cloud Run y tráfico, si aplican. No llamarlo «gratis» sin verificar cuotas vigentes y consumo previsto.
4. Esperar aprobación explícita antes de crear recursos, cambiar IAM, variables de entorno, tráfico, réplicas, balanceador o Cloud SQL.

Referencias: [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/), [vulnerabilidad GO-2026-6505](https://pkg.go.dev/vuln/GO-2026-6505), [diagnóstico de llamadas al sistema en Cloud Run](https://docs.cloud.google.com/run/docs/troubleshooting/tracing-system-calls).
