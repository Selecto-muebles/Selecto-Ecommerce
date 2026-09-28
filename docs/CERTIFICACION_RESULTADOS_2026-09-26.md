# Cierre comercial y certificación — Selecto

> Informe histórico del laboratorio del 26/09/2026. Su estado de pendientes fue
> actualizado después de la promoción y de las pruebas productivas en
> [CERTIFICACION_TECNICA_CIERRE_2026-09-28.md](CERTIFICACION_TECNICA_CIERRE_2026-09-28.md).

Este informe corresponde a las ramas de **Selecto-muebles**, antes de su merge y
despliegue. No equivale a certificar estos cambios en producción ni a autorizar
su publicación sin revisión. La base e infraestructura productivas no fueron
reemplazadas y los productos de clientes no fueron modificados.

## Implementado

- Alta por email/contraseña sin exigir DNI ni domicilio. Google permite confirmar
  la identidad verificada; conserva la transacción de identidad existente.
- Validación completa de entrega al comprar y snapshot por orden. La preferencia
  de pago conserva al comprador existente; usa el snapshot cuando faltan sus datos.
- Ayuda, contacto, entregas y posventa conectados a navegación. Los canales y
  condiciones reales son configuración pública, no datos comerciales inventados.
- Total de productos diferenciado del envío, fecha preferida sin promesa falsa y
  mensajes comerciales en lugar de explicaciones internas del backend.
- Sitemap XML dinámico y metadata de producto/categoría en el HTML inicial y
  durante navegación SPA. Escape de contenido, noindex de rutas privadas y
  fallback navegable si la API no responde, sin reflejar tokens ni cookies.

## Evidencia ejecutada

| Comprobación | Resultado y alcance |
| --- | --- |
| Go, race e integración PostgreSQL 17 | 176 pruebas/subpruebas PASS, 0 FAIL; un placeholder heredado SKIP. Rol limitado, 17 migraciones repetidas y auditoría de esquema aprobada. |
| Storefront | 97 PASS; un contrato público opt-in SKIP. Lint, TypeScript, build y presupuesto de bundle aprobados. |
| Admin | 49 PASS; lint, TypeScript, formato y build aprobados. 37 pruebas Playwright de contrato aprobadas. |
| Navegador con API/DB reales | Ingreso normal, categorías/orden/estado, alta con archivo de imagen real, publicación/remoderación, período KPI, carrito persistido, login, entrega, checkout simulado e historial. Sin mocks de rutas. |
| Stock concurrente | 40 solicitudes para stock 10: 10 reservas y 30 conflictos, sin sobreventa. 20 reintentos con la misma clave: una orden. Cancelación concurrente libera una sola vez. |
| Seguridad funcional | IDOR denegado, webhook sin firma rechazado, callback firmado duplicado idempotente, reseña sólo con compra pagada, revocación de sesión tras recuperación y cambio de rol inmediato. |
| Productos | Desactivar conserva el registro administrativo; DELETE independiente elimina un producto sin referencias. No se elimina historial comercial. |
| Carga local acotada | 500 lecturas, concurrencia 20, 500 HTTP 200. p50 9,30 ms; p95 11,98 ms; máximo 18,31 ms. No es una medición de capacidad o latencia GCP. |
| Runtime Nginx | Imagen fijada y configuración real: metadata inicial, sitemap y endpoint interno protegido. Segundo ensayo con upstream indisponible conserva shell y noindex. |
| Restauración local | pg_dump/pg_restore real de 21 tablas y 132 filas sintéticas; hashes de filas y secuencias coinciden; esquema restaurado auditado. No es restauración de Cloud SQL productivo. |
| Dependencias | npm audit sin vulnerabilidades en ambas UI; govulncheck ejecutado con Go 1.25.13, sin vulnerabilidades alcanzables del código. |

Evidencia privada del laboratorio: `/tmp/selecto-cert-NhDV0k/report.json`, informe
de navegador, salida de Go, cobertura y capturas en el mismo directorio. No se
versionan tokens, contraseñas, dumps ni datos de clientes. Se retiraron sólo los
contenedores temporales con identidad y etiqueta de propiedad comprobadas.

## Alertas: recepción NO certificada

El destinatario informó que el correo del ensayo no llegó. El canal está habilitado
y los dos logs NOTICE del ensayo fueron almacenados por GCP. La consulta de eventos
del canal no devolvió entradas durante el intervalo probado; la API de alertas
tampoco devolvió alertas al consultarla después de retirar la política temporal.
Esto no permite atribuir la causa a Gmail ni demostrar que se envió un correo.

La política temporal fue eliminada y su ausencia verificada. Los tres uptime
checks y las siete políticas operativas conservan la configuración versionada.
No se provocaron fallos productivos ni se cambiaron sus destinatarios. Recibo:
`/tmp/selecto-alert-drill-20260926-2235.json`, con `deliveryConfirmed: false`.

Procedimiento de diagnóstico de referencia:
[notificaciones faltantes de Cloud Monitoring](https://docs.cloud.google.com/monitoring/alerts/troubleshoot-missing-notifications).
La ausencia de eventos de error tampoco certifica una entrega correcta.

## Pendientes antes de comerciar y cerrar producción

1. Confirmar contacto atendido, cobertura, plazos y cotización/pago del envío.
2. Completar fotos, nombres, descripción y ficha técnica reales o autorizar el
   retiro de los productos de prueba. No se inventaron ni borraron esos datos.
3. Revisar/mergear/desplegar Backend, activar API Config nueva conservando rollback
   y sólo después mergear Storefront, cuyo despliegue es automático.
4. Aceptación productiva con sesiones normales, tras esa promoción.
5. Investigar y confirmar la recepción real de las alertas operativas.
6. Autorizar presupuesto e instancia temporal para restaurar un backup real de
   Cloud SQL fuera de producción; el laboratorio local no reemplaza ese ensayo.
7. Mobbex real y términos/condiciones legales, pendientes por separado.

Brevo no se volvió a probar contra contactos de clientes: la suscripción/baja ya
confirmada por el usuario permanece como evidencia del bloque anterior. El pago
de este laboratorio es simulado, sin cobro ni certificación de Mobbex.
