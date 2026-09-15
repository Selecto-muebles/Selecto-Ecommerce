# Categorías y destinos editoriales

El catálogo conserva `category` como nombre compatible con revisiones anteriores
y agrega `category_slug` como identificador de navegación. No cambia la base ni
los contratos de checkout, orden, descuento y stock.

La lista pública de categorías sólo muestra las que tienen al menos un producto
activo; el listado administrativo permanece completo. Los enlaces de un
carrousel cuyo destino es una categoría usan su slug, no el nombre textual, para
que sigan funcionando cuando se renombra la disciplina.

Promoción: merge manual Backend, workflow existente y verificación de
`/products`, `/categories` y `/carousel-slides`; luego Storefront. No se activa
una API Config nueva porque las rutas no cambian. No se crean recursos GCP.
