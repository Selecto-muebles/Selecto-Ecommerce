# Corrección operativa de productos

Fecha: 2026-09-13. Sin migraciones ni cambios de infraestructura.

- `DELETE /admin/products/:id` admite productos activos o inactivos sin historial de órdenes. No ejecuta una desactivación previa.
- Conserva bloqueo de fila, verificación de referencias, auditoría y eliminación en una única transacción. Un producto con órdenes devuelve 409 sin modificar estado ni historial. Éxito: 204 sin cuerpo.
- Auditoría incluye `was_active`. La UI exige escribir el nombre antes de eliminar definitivamente.
- `GET /admin/products` agrega `images` y `options` en el mismo formato del detalle. Se recuperan metadatos mediante las consultas por lote existentes, nunca binarios ni una consulta por producto. Esto permite miniaturas y evita vaciar opciones al editar desde la lista.
- Desactivar sigue conservando el registro y sus relaciones. El Admin muestra todos los estados por defecto.

## Verificación

Pruebas con PostgreSQL 16 local descartable, rol sin superusuario ni CREATE DATABASE, esquema `commerce`, las 15 migraciones existentes, `go test -count=1 -race ./...`.

Se cubren eliminación de activos/inactivos sin órdenes; rechazo con órdenes y conservación de estado/historial; auditoría; listado con imágenes/opciones. GitHub mantiene su matriz PostgreSQL 17/18 y gates existentes: no se omiten ni debilitan.

## Entrega

Merge manual. Desplegar Backend antes que Admin en los servicios existentes. No requiere API Config nueva ni recursos adicionales: se mantienen las rutas actuales. Certificación productiva pendiente del merge y despliegue; las pruebas locales no la sustituyen.
