# Certificación técnica de cierre — Selecto

Fecha de corte: **28/09/2026 (UTC)**
Organización: **Selecto-muebles**
Proyecto GCP reutilizado: **destry-development**

## Dictamen

La plataforma queda **certificada dentro del alcance técnico comprobado**. Los
flujos de identidad, catálogo, administración, comunicaciones, consistencia de
datos, recuperación y monitoreo tienen evidencia reproducible o productiva.

Esto no constituye todavía un **GO comercial irrestricto**. Permanecen fuera de
la certificación:

1. Mobbex productivo, hasta recibir y configurar los datos del comercio.
2. Términos, privacidad, cambios/devoluciones y demás textos aprobados legalmente.
3. Sustituir o retirar el contenido comercial de prueba que continúa publicado.

No se versionan credenciales, tokens, dumps, direcciones privadas ni datos de
clientes. Los nombres históricos `destry-*` de GCP son intencionales: alojan
Selecto y no implican desplegar sobre el proyecto independiente KubiGeek/Destry.

## Matriz actualizada

| Área auditada | Estado al 28/09 | Evidencia y límite |
| --- | --- | --- |
| Mensajes comerciales | **CERTIFICADO** | Checkout, entrega y totales usan texto dirigido al cliente; no explicaciones internas del backend. |
| Registro simplificado | **CERTIFICADO** | El alta no exige DNI ni domicilio; los datos de entrega se solicitan al comprar. |
| Contacto, ayuda, entrega y posventa | **CERTIFICADO TÉCNICO** | Las cuatro rutas son navegables y aparecen en el sitemap. Cobertura, plazos y condiciones concretas siguen siendo contenido del comercio. |
| Sitemap dinámico | **CERTIFICADO EN PRODUCCIÓN** | `https://selectosport.com/sitemap.xml` y la ruta de API responden 200 e incluyen portada, ayuda, contacto, entregas, posventa, productos y categoría. |
| Metadata y previews sociales | **CERTIFICADO EN PRODUCCIÓN** | El HTML inicial contiene canonical, description, Open Graph y Twitter Card de Selecto; las rutas de producto/categoría tienen contrato dinámico. |
| Contenido comercial real | **PENDIENTE COMERCIAL** | La API pública todavía expone `Cajion de salto` (`gcfgjfgj`) y `pararela` (`Lorem ipsum`), ambos sin imágenes ni especificaciones. No se inventaron ni eliminaron datos sin decisión del comercio. |
| E2E autenticado | **CERTIFICADO TÉCNICO** | Cuenta, login normal/Google, carrito, entrega, checkout simulado, orden, historial y Admin se probaron con API y PostgreSQL reales del laboratorio, sin mocks de rutas. Mobbex real queda excluido. |
| Stock, descuentos e idempotencia | **CERTIFICADO** | 40 solicitudes sobre stock 10 produjeron 10 reservas y 30 conflictos; 20 reintentos con una clave produjeron una orden; la cancelación concurrente liberó una sola vez. |
| Permisos y sesiones | **CERTIFICADO** | IDOR denegado, webhook sin firma rechazado, duplicados idempotentes, reseña limitada a compra pagada, recuperación revoca sesiones y el cambio de rol tiene efecto inmediato. |
| Carga controlada | **CERTIFICADO EN SU ALCANCE** | 500 lecturas, concurrencia 20 y 500 respuestas 200; p50 9,30 ms, p95 11,98 ms y máximo 18,31 ms en laboratorio. No representa capacidad ni latencia de GCP. |
| Restauración aislada | **CERTIFICADO CON BACKUP REAL** | Backup automatizado Cloud SQL `1790478000000` restaurado en una instancia temporal; auditoría, invariantes y limpieza aprobadas. |
| Recepción de alertas | **CERTIFICADO DE PUNTA A PUNTA** | Monitoring abrió el incidente controlado `0.od4p2mx9d3iu`; el destinatario confirmó recepción correcta. Política y métrica temporales fueron retiradas. |

Resultado de esta matriz: **11 puntos certificados y 1 pendiente comercial**.
Los límites expresos no convierten una prueba acotada en una certificación de
capacidad, de contenido legal ni de cobros externos.

## Versiones certificadas

| Repositorio | `main` observado |
| --- | --- |
| `Selecto-muebles/Selecto-Ecommerce` | `4f28ac295e45f6b6c6b6d3e6a88ccbbfa3b6ef63` |
| `Selecto-muebles/selecto-admin` | `cd344f70dd320a8efaae91bfea73e56c46ea07d6` |
| `Selecto-muebles/selecto-frontend` | `c6369464cceef148dfce91148750dcb3555a4b05` |

La corrección declarativa del uptime check y este informe pertenecen a una rama
posterior a esos SHA; deben atravesar preflight, revisión, merge y reconciliación
normal para pasar a formar parte de `main`.

## Evidencia funcional consolidada

- Backend: 176 pruebas/subpruebas aprobadas, race detector, PostgreSQL 17,
  migraciones repetibles y auditoría de esquema. Un placeholder heredado quedó
  omitido de manera explícita.
- Storefront: 97 pruebas aprobadas, lint, TypeScript, build y presupuesto de
  bundle. El contrato público opt-in omitido está identificado.
- Admin: 49 pruebas unitarias y 37 pruebas Playwright de contrato aprobadas,
  además de lint, TypeScript, formato y build.
- Brevo: dominio `selectosport.com` autenticado, DKIM/DMARC activos, remitente
  `Selecto <no-reply@selectosport.com>`, recuperación recibida en Principal y
  suscripción/baja de newsletter confirmadas por el destinatario.
- Identidad: acceso con Google confirmado por un usuario real; la vinculación de
  identidad, auditoría y verificación quedó agrupada transaccionalmente.
- Storefront, Admin, `GET /ready` y `GET /health` respondieron HTTP 200 durante
  este corte. La pantalla de carga y la metadata Selecto están presentes en el
  HTML inicial público.

## Restauración real de Cloud SQL

Fuente: instancia productiva `destry-postgres-staging`, PostgreSQL 17. Se usó el
backup automatizado `1790478000000`, iniciado el 27/09/2026 a las 05:24:34 UTC y
finalizado exitosamente a las 05:26:25 UTC.

El backup se restauró en `selecto-restore-cert-20260927`, una instancia temporal
aislada sin redes autorizadas, con conexión cifrada y sin reemplazar producción.
La conexión se realizó mediante Cloud SQL Auth Proxy y se ejecutó el auditor del
backend desplegado.

| Invariante restaurada | Resultado |
| --- | ---: |
| Tablas de comercio | 21 |
| Migraciones | 17 |
| Índices requeridos | 15 |
| Filas exactas inspeccionadas | 77 |
| Claves primarias | 21 |
| Claves foráneas | 17 |
| Secuencias | 18 |
| Índices inválidos | 0 |

Dos restricciones históricas de proveedor están declaradas `NOT VALID`, pero la
consulta de datos existentes obtuvo cero violaciones en órdenes y webhooks. No se
alteró el esquema para ocultar esa observación.

La operación de restauración finalizó `DONE`. Luego se cerró el proxy, se eliminó
la instancia temporal y se comprobó que sólo permanecía la instancia productiva,
en estado `RUNNABLE`, con backup y recuperación a un punto en el tiempo activos.
El backup fuente continúa `SUCCESSFUL`.

## Monitoreo y recepción

- Tres uptime checks consultan Storefront, Admin y API desde Iowa, Bélgica y São
  Paulo cada cinco minutos.
- Siete políticas cubren HTTPS/TLS, certificados, HTTP 5xx, jobs fallidos o sin
  éxito y errores de correo/Scheduler.
- El canal de email `15313479877170240331` está habilitado y asociado a todas las
  políticas operativas.
- La prueba creó una métrica contador y una política temporales, escribió un
  evento inequívocamente identificado como ensayo y abrió el incidente
  `projects/destry-development/alerts/0.od4p2mx9d3iu` a las 21:36:20 UTC del
  27/09/2026. El destinatario confirmó la recepción correcta del correo.
- La política y la métrica de ensayo fueron eliminadas y su ausencia fue
  comprobada. No se provocó una caída ni se modificaron pedidos o comunicaciones.

Durante la comprobación se detectó un falso incidente del Storefront: las tres
regiones recibían HTTP 200, pero el matcher de título marcaba `content_mismatch`.
Se retiró únicamente ese matcher; HTTP 200, TLS, regiones, período y timeout se
mantuvieron. Las tres regiones pasaron a verde y el incidente
`0.od3wufjgpqwy` cerró automáticamente el 27/09/2026 a las 21:50:17 UTC.
`logCheckFailures=true` permanece activo para diagnóstico.

## Datos, seguridad y operación

- La instancia productiva no se restauró, clonó sobre sí misma ni quedó expuesta
  públicamente durante el ensayo.
- No se ejecutaron compras, pagos, reembolsos ni borrados de clientes como parte
  de esta certificación.
- Las imágenes, categorías, carrouseles, reseñas, KPI y comunicaciones poseen
  contratos administrativos. Desactivar y eliminar productos son operaciones
  diferentes; el historial comercial impide eliminaciones destructivas.
- Newsletter usa outbox e idempotencia; la baja es confirmada, de un solo uso y
  sincronizada con Brevo. Webhooks y supresiones conservan trazabilidad.
- Los recursos temporales de PostgreSQL, Monitoring y scripts de verificación
  fueron retirados. Los recibos privados permanecen fuera de Git.

## Condiciones para declarar GO comercial

1. Cargar o retirar los dos productos de prueba y revisar fotografías,
   especificaciones, precios, stock y condiciones de entrega reales.
2. Incorporar y aprobar los textos legales aplicables.
3. Configurar Mobbex productivo cuando estén disponibles los datos del comercio;
   ejecutar una compra y webhook reales controlados antes de habilitar cobros.
4. Ejecutar smoke posterior a cada despliegue y conservar rollback de imagen y
   API Config. No modificar DB, JWT o secretos durante un rollback ordinario.

Hasta completar esos tres insumos comerciales/externos, el resultado correcto es
**plataforma técnicamente certificada, publicación comercial condicionada**.
