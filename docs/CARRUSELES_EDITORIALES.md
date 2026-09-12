# Carrusel editorial de portada

## Modelo y límites

- Migración **015**, aditiva y repetible; no modifica datos comerciales existentes.
- `carousel_slides`: título, descripción, alt, CTA, orden, estado, versión, destino FK a producto o categoría e imagen binaria PNG/JPEG (2 MB máximo).
- Máximo 20 registros contando borradores, serializado con advisory lock transaccional. No agrega servicios, buckets, jobs, secretos ni instancias.
- Es el mismo patrón de almacenamiento binario existente en galerías, acotado a 40 MB de imágenes editoriales por colección. Backups y versiones históricas de PostgreSQL pueden consumir espacio adicional.
- Sin URLs externas arbitrarias, HTML, SVG, autoplay ni código configurable por el operador.
- Las bajas de producto/categoría hacen SET NULL: no impiden eliminar un producto ni dejan enlaces públicos rotos. Los destinos inactivos y las categorías sin productos activos se ocultan al leer. Un cambio posterior de disponibilidad no altera la intención de publicación del operador.
- Las imágenes de borradores requieren autenticación administrativa. Las públicas se sirven sólo mientras diapositiva y destino estén disponibles. `no-store` evita prolongar publicaciones retiradas; no se agregan cachés ni servidores.

## Contratos

| Ruta | Métodos | Uso |
|---|---|---|
| `/carousel-slides` | GET, OPTIONS | `{items:[]}`, máximo 20, orden por sort_order e id |
| `/carousel-images/{id}` | GET, OPTIONS | Imagen pública disponible o 404 |
| `/admin/carousel-slides` | GET, POST, OPTIONS | Lista completa y alta auditada |
| `/admin/carousel-slides/{id}` | PATCH, DELETE, OPTIONS | Edición y eliminación con versión |
| `/admin/carousel-slides/{id}/image` | GET, OPTIONS | Vista previa autenticada, incluso borradores |

POST/PATCH usan multipart con `payload` JSON y archivo `image`. El archivo es obligatorio al crear, opcional al editar. El payload completo es:

```json
{"title":"Texto real","subtitle":"","alt_text":"Descripción real","cta_label":"Ver producto","target_kind":"product","target_id":"ID_PUBLICO","sort_order":0,"active":false,"version":0}
```

`target_id` de producto usa el identificador público existente; el de categoría es el ID decimal como string. PATCH requiere la versión leída (>=1); DELETE exige `?version=N`. Error 400 para datos inválidos, 404 para diapositiva inexistente y 409 ante versión obsoleta, límite de registros o destino no publicable. No reintentar automáticamente una escritura con 409.

Guardar/eliminar y registrar auditoría ocurren en una sola transacción. Una falla en auditoría revierte el cambio. No se suben imágenes o texto comercial inventados a producción.

## Promoción manual

1. Revisar y mergear Backend; ejecutar el workflow existente en main. Migración/auditoría esperan 15 migraciones, 17 tablas y 10 índices auditados.
2. Comparar el OpenAPI contra el Gateway ACTIVE: mantener las 50 rutas previas y añadir las 5 nuevas (55 en total); backend e IAM sin cambios. Crear configuración inmutable por SHA y activarla manualmente. Guardar el ID ACTIVE anterior para rollback; no borrarlo.
3. Verificar público 200/404 y administrativo 401/403; CORS POST/PATCH/DELETE. No crear datos de prueba en producción.
4. Merge manual y workflow Admin. Merge manual Storefront sólo después de revisar la vista local: su workflow puede desplegar automáticamente main.
5. Con sesión administrativa normal, cargar contenido comercial real como borrador, revisar vista previa y publicar. Confirmar destino, móvil, ocultación y recuperación ante fallos. Una suite con mocks no certifica producción.

Rollback: volver a imágenes anteriores y a la API Config previamente capturada. **No ejecutar down ni borrar tabla/migración**, porque las versiones anteriores ignoran la tabla nueva. Storefront mantiene portada habitual si la API editorial no existe, falla o está vacía; Novedades no cambia.

## Pruebas

- Suite Go race y rol restringido sin CREATE DATABASE, migración doble y auditoría.
- Login HTTP real local → categoría → producto con FK → carrusel → imagen; autorización dinámica por rol.
- Versiones obsoletas, borradores no públicos, destinos inactivos/eliminados y rollback real de auditoría.
- Validación de formatos, límites y URLs internas; navegador Admin con contratos aislados y storefront sin autoplay.
- Pendiente de certificación externa: sesión normal de producción y aprobación visual del operador.
