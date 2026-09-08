# Descuentos por cantidad

El backend es la autoridad del importe cobrado. El Storefront muestra una
estimación con la misma regla, pero `POST /orders` vuelve a leer precio, estado y
stock bajo bloqueo transaccional antes de reservar unidades y persistir el total.

## Escala vigente

| Cantidad total del mismo producto | Descuento |
| --- | ---: |
| 1 | 0 % |
| 2 a 3 | 5 % |
| 4 a 9 | 10 % |
| 10 a 100 | 15 % |

Las variantes se agrupan por `product_id`: cuatro unidades del mismo producto
reciben 10 %, aunque estén distribuidas entre colores o modelos distintos. Los
productos diferentes no suman cantidades entre sí.

El precio se calcula en centavos enteros y el descuento unitario se redondea al
centavo más cercano, con medios centavos hacia arriba. En `order_items`, `price`
es el precio unitario efectivamente cobrado; `original_unit_price` y
`discount_percent` conservan la evidencia comercial de la operación.

La reserva de stock, el precio, los ítems, la orden, la auditoría y el outbox se
confirman en una sola transacción PostgreSQL. Ante cualquier error se ejecuta
rollback y no queda stock descontado ni una orden parcial.
