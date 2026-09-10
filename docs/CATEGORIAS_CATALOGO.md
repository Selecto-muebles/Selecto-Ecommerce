# Categorías: primera entrega

La migración 014 agrega categories y products.category_id con FK restrictiva.
El trigger transaccional conserva category como proyección textual compatible
con las revisiones anteriores y normaliza coincidencias por lower(btrim(name)).
Una escritura antigua con un nombre nuevo crea una categoría dentro de la misma
transacción del producto. Esta compatibilidad es deliberada durante la transición.
Los slugs de backfill usan un hash determinista para evitar colisiones al transliterar.
Las altas explícitas permiten elegir un slug legible y rechazan duplicados con 409.

GET /categories y GET /admin/categories devuelven items, page, page_size, has_more.
POST /admin/categories recibe name y slug; requiere sesión administrativa.
Las altas y la auditoría se confirman o revierten conjuntamente.

Orden de promoción: merge manual Backend, workflow/migración 014, activación de
API Config con las rutas nuevas y OPTIONS, smoke autenticado; luego merge Admin.
No cambiar IAM, capacidad de Cloud Run, base de datos ni secretos.
Rollback de aplicación: la versión anterior puede seguir utilizando category.
No eliminar la tabla ni ejecutar una migración inversa tras recibir datos nuevos.

Esta entrega incluye alta, listado y selección. Edición/archivo de categorías y
consumo de slug/ID en Storefront quedan para una entrega posterior; no se anuncian
como disponibles. Carruseles editoriales, reseñas y KPI siguen pendientes.
