# Experiencia comercial y certificación

Organización: **Selecto-muebles**. Los recursos productivos históricos `destry-*`
se conservan. Este bloque no agrega migraciones ni requiere otra base productiva.

## Promoción, en orden

1. Revisar y mergear Ecommerce; desplegar su `main` mediante el workflow habitual.
2. Activar la nueva API Config en el Gateway existente, conservando la anterior.
   Las nuevas rutas públicas son `GET /seo/head?path=...` y `GET /sitemap.xml`.
3. Sólo después, mergear Storefront: su workflow despliega automáticamente `main`.
   El registro corto requiere el nuevo contrato del backend.
4. Admin incorpora herramientas de certificación y una actualización de Vitest,
   no cambios a las pantallas operativas ni otro servicio.
5. Comprobar sitemap y metadata desde HTML sin ejecutar JavaScript; probar alta,
   verificación, ingreso y compra con una sesión normal tras la promoción.

## Laboratorio reproducible

Requisitos: Docker, PostgreSQL 17 (imagen `postgres:17-alpine`), Go 1.25.13,
Node 22.23.1, dependencias `npm ci` de Admin y Storefront, Chromium de Playwright.
No necesita una conexión a Cloud SQL ni credenciales de Google, SMTP o Brevo.

Desde Ecommerce:

```bash
node --test scripts/certification/safety.test.mjs
LAB_ADMIN_DIR=/ruta/selecto-admin \
LAB_STOREFRONT_DIR=/ruta/selecto-frontend \
node scripts/certification/lab.mjs
```

`PLAYWRIGHT_CHROMIUM_EXECUTABLE` permite usar un Chromium instalado. Sin los dos
directorios UI, sólo se ejecuta el laboratorio HTTP/SQL; el informe no afirma
que haya pasado el navegador. Las pruebas reales no usan `page.route`, JWT
fabricados ni escritura manual de sesiones. El alta administrativa de prueba
se siembra únicamente en la DB sintética y el ingreso pasa por `/login`.

El laboratorio crea su propio contenedor PostgreSQL con datos en tmpfs,
identificador aleatorio y puerto publicado sólo en loopback. El rol de ejecución
no es superusuario ni tiene acceso al esquema Payments. Aplica las migraciones
dos veces, audita el esquema y ejecuta `go test -race` con integración habilitada.
No usa ni elimina los contenedores existentes del usuario.

Ejercita registro mínimo, verificación por outbox local, login normal, stock y
descuentos, idempotencia, callback firmado y duplicado, reseñas verificadas,
permisos/IDOR y revocación de sesión. La pasarela es **simulada** en loopback:
no certifica Mobbex ni cobra fondos. SMTP, Brevo y workers externos están apagados.
La carga está limitada a 500 lecturas con concurrencia 20, nunca a producción.

Después de detener las escrituras, hace un `pg_dump` real y restaura en una
segunda base aislada. Compara hashes de todas las filas, secuencias y auditoría
de índices/restricciones. Esto certifica la recuperación del conjunto sintético,
**no la restauración de un backup productivo de Cloud SQL**.

Los informes y capturas se generan en `/tmp/selecto-cert-*`; los diagnósticos
son privados. El contenedor tmpfs se elimina sólo después de verificar su etiqueta
de propiedad. Si se interrumpe forzosamente el proceso, revisar las etiquetas
`selecto.cert.owner` antes de limpiar: nunca borrar por un comodín.

## Pendientes que no se pueden dar por cerrados con código

- Contacto comercial real, cobertura, plazos y cotización/pago del envío.
- Fotos, descripción, especificaciones y decisión sobre los productos de prueba.
  No se fabrican datos comerciales ni se alteran productos productivos por este script.
- Prueba productiva de los cambios después de merges y despliegues.
- Restauración de un backup de Cloud SQL en instancia temporal: requiere
  autorización y presupuesto. Nunca restaurar sobre la instancia productiva.
- Recepción efectiva de alertas por el destinatario: crear una política o aceptar
  un log no equivale a demostrar que el correo llegó.

El ensayo de notificación usa una política temporal independiente y un evento
NOTICE identificado como prueba, sin provocar una caída. Para recuperar la
limpieza después de una interrupción:

```bash
GCLOUD_BIN=/ruta/gcloud node scripts/gcp/alert_drill.mjs \
  --cleanup --receipt=/tmp/recibo-del-ensayo.json
```

El script sólo elimina la política con la organización y etiqueta de propiedad
del recibo. No cambia políticas operativas ni destinatarios existentes.
