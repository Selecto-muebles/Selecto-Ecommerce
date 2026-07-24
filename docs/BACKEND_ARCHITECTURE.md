# Arquitectura del backend Ecommerce

## Objetivo

El backend se despliega como una unica API HTTP stateless en Cloud Run, expuesta por API Gateway. Esta unidad de despliegue conserva las transacciones locales, reduce cold starts y puede escalar a cero cuando no recibe trafico. Los workers periodicos se ejecutan como Cloud Run Jobs sin convertir cada endpoint en una funcion independiente.

## Fronteras

### `internal/delivery/http`

Adaptador de entrada. Define rutas, autentica, enlaza payloads, traduce errores y serializa respuestas. No debe contener algoritmos de negocio ni implementar clientes externos.

### `internal/service`

Casos de uso y reglas independientes del transporte. Ordenes, checkout, pagos, catalogo, envios y administracion exponen funciones o servicios testeables sin Gin. Las transformaciones en memoria pueden usar `Map`, `Filter`, `Reduce` y ordenamiento compartido cuando mejoran claridad.

### `internal/repository/postgres`

Persistencia PostgreSQL. Encapsula queries, locks, idempotencia y unidades transaccionales. La creacion de orden conserva en una sola transaccion el bloqueo de productos, la reserva de stock, la direccion, los items, auditoria y outbox.

### `internal/infrastructure`

Adaptadores externos y runtime: pool PostgreSQL, correo, validacion de identidad Google y cliente HTTP de Payments. Timeouts y pools se configuran desde `config`.

### `internal/domain` y `internal/shared`

Estados, transiciones, value objects y utilidades sin dependencias de entrega o infraestructura.

## Reglas de dependencia

1. HTTP puede depender de servicios, repositorios e infraestructura para componer adaptadores.
2. Servicios dependen de interfaces y del dominio, no de Gin.
3. Repositorios implementan puertos de servicios y pueden depender de PostgreSQL.
4. Infraestructura implementa integraciones externas y no decide reglas comerciales.
5. Filtros, ordenamiento y paginacion que pertenecen a SQL permanecen en PostgreSQL. No se trasladan a memoria para aparentar programacion funcional.

## Escalabilidad

- La API no mantiene estado de sesion en memoria.
- La idempotencia de ordenes usa advisory locks y clave persistida.
- Stock se protege con transacciones y `FOR UPDATE`.
- Checkout reutiliza preferencias y resuelve carreras al persistir.
- El cliente de Payments reutiliza conexiones HTTP.
- Google JWKS posee cache concurrente con expiracion.
- Los archivos Go se limitan a 300 lineas mediante CI.

## Extension

Un nuevo caso de uso debe comenzar en `service`, declarar su puerto de persistencia y obtener una implementacion en `repository/postgres`. La ruta HTTP solamente adapta el contrato congelado. Este esquema permite incorporar otro adaptador de entrada o ejecutar un caso de uso desde un job sin duplicar negocio.
