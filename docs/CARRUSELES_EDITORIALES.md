# Carrusel editorial de portada

## Modelo y límites

- Migración **015** para originales y **028** para multimedia/derivados, aditivas y repetibles; no modifican los datos comerciales existentes.
- `carousel_slides`: título, descripción, alt, CTA, orden, estado, versión, destino FK a producto o categoría e imagen original PNG/JPEG (2 MB máximo). `carousel_media` conserva derivados responsive de 960/1920 px y un MP4 opcional (H.264, 60 segundos, 20 MB).
- Máximo 20 registros contando borradores, serializado con advisory lock transaccional. No agrega servicios, buckets, jobs, secretos ni instancias.
- Es el mismo patrón de almacenamiento binario existente en galerías. A los originales se suman derivados y clips opcionales; backups y versiones históricas de PostgreSQL pueden consumir espacio adicional.
- Sin URLs externas arbitrarias, HTML, SVG ni código configurable por el operador. No rota diapositivas ni reproduce clips al abrir la página: el visitante decide cuándo reproducir.
- Las bajas de producto/categoría hacen SET NULL: no impiden eliminar un producto ni dejan enlaces públicos rotos. Los destinos inactivos y las categorías sin productos activos se ocultan al leer. Un cambio posterior de disponibilidad no altera la intención de publicación del operador.
- Los borradores requieren autenticación administrativa. Las rutas públicas comprueban la disponibilidad de diapositiva y destino; la lista usa `no-store`. Las portadas y clips públicos permiten caché de navegador por 60 segundos con ETag; un archivo ya descargado puede permanecer en esa caché durante el minuto. Las vistas privadas usan caché privada y revalidación. No se agrega infraestructura de caché.
- Los originales se conservan. Los derivados se preparan al cargar una portada y, para registros anteriores, en su primera lectura. Se conservan transparencias y orientación de cámara; la transformación está acotada y serializada por instancia. Los registros anteriores demasiado grandes para transformar conservan su imagen original como respaldo.

## Contratos

| Ruta | Métodos | Uso |
|---|---|---|
| `/carousel-slides` | GET, OPTIONS | `{items:[]}`, máximo 20, orden por sort_order e id |
| `/carousel-images/{id}` | GET, OPTIONS | Imagen pública disponible o 404 |
| `/admin/carousel-slides` | GET, POST, OPTIONS | Lista completa y alta auditada |
| `/admin/carousel-slides/{id}` | PATCH, DELETE, OPTIONS | Edición y eliminación con versión |
| `/admin/carousel-slides/{id}/image` | GET, OPTIONS | Vista previa autenticada, incluso borradores |

POST/PATCH usan multipart con `payload` JSON y archivo `image`. La portada es obligatoria al crear, opcional al editar. El archivo `video` es opcional; `remove_video: true` permite quitarlo conservando la portada. `GET .../image?w=960` y `?w=1920` sirven derivados; `?media=video` sirve el clip con rangos HTTP 206. Las rutas anteriores sin estos parámetros conservan los originales. El payload básico es:

```json
{"title":"Texto real","subtitle":"","alt_text":"Descripción real","cta_label":"Ver producto","target_kind":"product","target_id":"ID_PUBLICO","sort_order":0,"active":false,"version":0}
```

`target_id` de producto usa el identificador público existente; el de categoría es el ID decimal como string. PATCH requiere la versión leída (>=1); DELETE exige `?version=N`. Error 400 para datos inválidos, 404 para diapositiva inexistente y 409 ante versión obsoleta, límite de registros o destino no publicable. No reintentar automáticamente una escritura con 409.

Guardar/eliminar y registrar auditoría ocurren en una sola transacción. Una falla en auditoría revierte el cambio. No se suben imágenes o texto comercial inventados a producción.

## Promoción manual

1. Primero completar la entrega de videos de productos (Ecommerce #114, Storefront #61, Admin #86). Esta entrega parte de esas ramas.
2. Revisar y mergear Backend del carrusel; ejecutar su workflow habitual. Se aplica la migración 028 antes de cambiar tráfico. El contrato mantiene las 95 rutas: no necesita otra configuración de Gateway ni cambios de IAM.
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
