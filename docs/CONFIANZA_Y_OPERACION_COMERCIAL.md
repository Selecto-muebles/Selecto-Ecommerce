# Confianza y operación comercial

## Alcance

La migración `016_product_trust_and_operations.sql` agrega información comercial estructurada, reseñas verificadas y archivado operativo sin introducir servidores ni servicios nuevos.

## Productos y archivo

- `products.specifications` almacena hasta 24 pares de atributo y valor.
- `products.archived_at` separa el historial contable del catálogo operativo.
- `DELETE /admin/products/{id}` elimina físicamente productos sin órdenes.
- Si existen órdenes, el mismo endpoint desactiva y archiva el producto en una transacción y responde `204` con `X-Deletion-Mode: archived`.
- Los listados públicos y el listado administrativo predeterminado excluyen archivados.
- El artefacto de staging `RBM-STG-001` se archiva por coincidencia exacta de SKU y nombre durante la migración.

## Reseñas

- `POST /products/{id}/reviews` exige sesión y una orden pagada del usuario para ese producto.
- Existe una sola reseña por usuario y producto. Una edición vuelve a estado `pending`.
- `GET /products/{id}/reviews` sólo publica elementos moderados y no expone emails.
- `GET /admin/product-reviews` y `PATCH /admin/product-reviews/{id}` permiten moderar con auditoría atómica.
- No se generan estrellas, testimonios ni compras ficticias.

## Métricas

`GET /admin/dashboard` conserva los campos previos y agrega ticket promedio, unidades vendidas, comparación mensual, órdenes demoradas/fallidas, stock bajo, clientes nuevos/recurrentes, comunicaciones y productos más vendidos. Los cortes diarios y mensuales usan `America/Argentina/Buenos_Aires`.

Conversión y abandono siguen fuera del contrato: requieren instrumentación de eventos de navegación y no deben inferirse de órdenes.
