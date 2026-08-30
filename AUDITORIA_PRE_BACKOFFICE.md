# Auditoria Pre Backoffice - Selecto

Fecha: 2026-07-02
Revision actualizada: 2026-07-03

## Decision

El sistema esta en condiciones de pasar a la etapa de Selecto Admin y desarrollo de backoffice.

No se detectan bloqueantes criticos de funcionalidad en ecommerce ni en payments para avanzar con backoffice, smoke tests ampliados y stress testing controlado.

## Alcance Auditado

- Selecto-Ecommerce
- selecto-payments
- Comunicacion ecommerce -> payments
- Comunicacion payments -> ecommerce
- Webhooks internos y externos
- Registro de usuarios con datos de facturacion/envio
- Permisos admin
- Catalogo y ordenes
- Logs estructurados
- Tests automatizados
- Estado operativo local

## Estado De Repositorios

### Selecto-Ecommerce

- Rama auditada: `pre-produccion`
- Ultimo commit: `de2168f version preprod lista: registro completo, permisos, logs y flujo de pagos OK`
- Working tree: limpio
- Tests: OK

Comando ejecutado:

```bash
go test ./...
```

Resultado:

```text
ok Selecto-Ecommerce/tests/domain
ok Selecto-Ecommerce/tests/handlers
ok Selecto-Ecommerce/tests/integration
ok Selecto-Ecommerce/tests/middleware
ok Selecto-Ecommerce/tests/money
ok Selecto-Ecommerce/tests/utils
```

### selecto-payments

- Rama auditada: `pre-produccion`
- Ultimo commit: `bbc2284 Endurecer payments para preproduccion y modularizar arquitectura`
- Working tree: limpio
- Tests: OK

Comando ejecutado:

```bash
go test ./...
```

Resultado:

```text
ok selecto-payments/tests/http
ok selecto-payments/tests/services
```

## Estado Operativo Local

### Ecommerce

Ecommerce esta levantado y responde correctamente.

```http
GET http://localhost:8080/health
200 OK
{"status":"ok"}
```

### Payments

Payments esta levantado y responde correctamente.

```http
GET http://localhost:8081/health
200 OK
{"status":"ok"}
```

```http
GET http://localhost:8081/ready
200 OK
{"status":"ready"}
```

Puertos auditados:

- Ecommerce: `:8080`
- Payments: `:8081`
- Base local: `:5433`

No se detectan conflictos de puertos ni servicios cruzados.

## Configuracion Entre Servicios

La configuracion mutua esta alineada.

### Ecommerce -> Payments

```env
PAYMENTS_SERVICE_URL=http://localhost:8081
```

### Payments -> Ecommerce

```env
BACKEND_WEBHOOK_URL=http://localhost:8080/payments/webhook
```

### Firma Interna

El secreto interno coincide entre ambos servicios:

```env
INTERNAL_WEBHOOK_SECRET == ECOMMERCE_WEBHOOK_SECRET
```

### Mercado Pago -> Payments

```env
MP_NOTIFICATION_URL=https://anemic-duvet-pronounce.ngrok-free.dev/webhook
```

La URL es HTTPS publica y termina en `/webhook`, que es el contrato esperado para Mercado Pago.

## Cambios Validados En Ecommerce

### Registro

El registro ahora requiere datos necesarios para facturacion y envio:

- Nombre/s
- Apellido/s
- DNI
- Direccion
- Numero de direccion
- Codigo postal
- Provincia
- Localidad
- Numero de telefono

La validacion fue modularizada en `internal/shared/validation`.

### Permisos Admin

Los endpoints admin ya no dependen solamente del rol embebido en el JWT.

Se agrego validacion contra base de datos mediante middleware dedicado:

```text
internal/delivery/http/middleware/admin.go
```

Esto reduce el riesgo de tokens viejos con privilegios admin luego de cambios de rol.

### Catalogo Y Ordenes

Se corrigio el riesgo de productos inactivos:

- El catalogo lista productos activos.
- La creacion de ordenes no permite reservar productos inactivos.
- Los errores de lectura de catalogo ya no devuelven respuestas parciales con `200 OK`.

### Rate Limit

El rate limit en memoria ahora limpia buckets viejos de forma periodica.

Esto reduce crecimiento indefinido de memoria durante stress testing o trafico con muchas IPs.

### Logs

Se incorporaron logs estructurados profesionales en ingles.

Los nombres de eventos fueron centralizados para facilitar migracion futura a CloudWatch, SQS o pipelines observables.

### Tests

Los tests fueron movidos fuera de directorios principales hacia `tests/`, manteniendo cobertura automatizada y ordenada.

## Cambios Validados En Payments

Segun el estado actual del repo, runtime local y tests:

- Payments fue endurecido para preproduccion.
- El servicio esta levantado en `:8081`.
- `GET /health` responde `200 OK`.
- `GET /ready` responde `200 OK`.
- Webhooks estan cubiertos por tests HTTP y de servicios.
- Se modularizo arquitectura por responsabilidades.
- Se incorporo logging estructurado con `log/slog`.
- Se centralizaron eventos de logging en `internal/shared/logging`.
- Se agregaron helpers reutilizables en `internal/shared/collection`.
- La validacion de firma Mercado Pago fue aislada en `internal/services/signature.go`.
- La comunicacion hacia ecommerce fue aislada en `internal/services/ecommerce.go`.
- El manejo de retry/backoff hacia ecommerce esta separado del handler HTTP.
- La logica de webhook fue separada en `internal/services/webhook.go`.
- La logica de preference/payment fue separada en modulos de servicio dedicados.
- Los handlers HTTP quedaron mas finos: parsean, delegan y responden.
- Se eliminaron rastros de logs sueltos tipo `log.Printf`, `log.Fatal`, `fmt.Print` en `internal`, `cmd` y `tests`.
- No se detectaron patrones de `force` o cambios forzados de estado.
- El endpoint publico `/webhook` rechaza sin firma con `401 INVALID_SIGNATURE`, como corresponde.

Punto importante: no se debe marcar un pago como `approved` ni una orden como `paid` salvo que Mercado Pago lo informe realmente y el webhook/consulta validada lo procese.

Tests destacados observados:

- Firma valida e invalida de Mercado Pago.
- Configuracion invalida.
- Payment `approved` notifica ecommerce.
- Payment `pending` no notifica ecommerce como pago aprobado.
- Webhook duplicado mantiene idempotencia.
- Evento fallido queda retryable.
- Fallo retryable contra ecommerce conserva notificacion pendiente.
- Notificacion idempotente evita duplicados.

## Validacion Del Flujo De Pago

El flujo esperado se mantiene:

```text
Mercado Pago
  -> selecto-payments /webhook
  -> selecto-payments valida firma/evento/idempotencia
  -> selecto-payments notifica ecommerce
  -> Selecto-Ecommerce /payments/webhook
  -> ecommerce valida firma interna
  -> ecommerce actualiza orden si la transicion es valida
```

La regla de integridad sigue vigente:

```text
No se fuerzan pagos.
No se fuerzan ordenes a paid.
Los estados cambian por eventos validos.
```

## Riesgos Criticos Revisados

### Migracion De Registro

Resuelto.

La migracion de customer profile fue preparada sin imponer `NOT NULL` de forma peligrosa para rolling deploy. Esto evita romper app vieja o nueva durante despliegue.

### Productos Inactivos

Resuelto.

Productos inactivos ya no deben ser visibles/comprables desde el flujo publico.

### Catalogo Parcial

Resuelto.

Errores de scan o rows ahora deben tratarse como error real y no como catalogo incompleto exitoso.

### Admin Por JWT Viejo

Resuelto.

Los permisos admin se revalidan contra DB.

### Rate Limit En Memoria

Mitigado.

Tiene limpieza de memoria. Para produccion escalada horizontalmente, evaluar Redis/API Gateway/WAF.

### Validacion De Datos De Cliente

Resuelto en capa modular.

La validacion de DNI, codigo postal y telefono fue reforzada y extraida de handlers.

## Riesgos Restantes No Bloqueantes

### Smoke End-To-End Manual

Payments ya esta levantado y responde correctamente. El proximo smoke end-to-end debe validar nuevamente el circuito completo con una compra nueva:

- crear orden
- checkout
- redireccion a Mercado Pago
- pago aprobado por Mercado Pago
- webhook en payments
- notificacion interna a ecommerce
- orden `paid`
- idempotencia ante retry

### Rate Limit Distribuido

El rate limit local esta bien para develop/preprod y stress inicial, pero para produccion con multiples instancias conviene moverlo a infraestructura compartida.

Opciones:

- Redis
- API Gateway
- WAF
- Load balancer con rate limiting

### Observabilidad Productiva

Los logs ya estan preparados conceptualmente, pero para produccion falta definir destino:

- CloudWatch Logs
- SQS/EventBridge para eventos asincronicos
- dashboards por eventos criticos
- alertas por 5xx, webhook failures, payment notification failures y rejected signatures

## Checklist Para Pasar A Selecto Admin

- Ecommerce compila y testea: OK
- Payments compila y testea: OK
- Repos limpios: OK
- Registro completo para cliente: OK
- Permisos admin reforzados: OK
- Productos inactivos protegidos: OK
- Webhooks internos protegidos por firma: OK
- Logs estructurados: OK
- Tests movidos a `tests/`: OK
- Sin cambios forzados de pagos/ordenes: OK
- Payments levantado en `:8081`: OK
- Payments `ready`: OK
- Conexiones locales mutuas sin conflicto: OK

## Recomendacion

Avanzar con Selecto Admin/backoffice.

Prioridad sugerida para backoffice:

1. Login/admin session.
2. Dashboard operativo.
3. CRUD de productos.
4. Activar/desactivar productos.
5. Gestion de stock.
6. Listado y detalle de ordenes.
7. Estado de pagos y trazabilidad de webhooks.
8. Vista de clientes y datos de envio/facturacion.
9. Panel de errores operativos.
10. Acciones administrativas auditables.

## Veredicto

Selecto queda listo para iniciar desarrollo de backoffice.

El proximo hito recomendado es construir Selecto Admin sobre los contratos actuales, sin modificar el flujo de pagos ya validado, y luego ejecutar smoke tests completos mas stress testing progresivo.
