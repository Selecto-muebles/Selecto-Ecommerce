# Cuotas de navegación detrás del balanceador

## Incidente comprobado

El 12 de noviembre de 2026, a las 10:15–10:16 de Buenos Aires, una ráfaga
de consultas a `/seo/head` agotó el cupo compartido con `/categories` y
`/products`. Se correlacionaron las 12 respuestas 429 del navegador Android
con sus trazas del backend. Los registros de aplicación identificaban todas
las solicitudes como el mismo peer interno de Cloud Run.

No era una restricción propia del teléfono: el limitador usaba `ClientIP()`
con solo loopback como proxy confiable. Aumentar el límite global o confiar
en cualquier `X-Forwarded-For` dejaría sin resolver el aislamiento o permitiría
falsificar la identidad.

## Comportamiento del código

- Se conservan las cuotas por instancia de un minuto. `RATE_LIMIT_PER_MINUTE`
  sigue siendo 120; no es un límite global distribuido entre réplicas.
- Catálogo, multimedia, metadatos SEO, operaciones y webhooks tienen cupos
  independientes. El de multimedia admite cinco veces el límite base para
  miniaturas y tamaños responsivos. Todos conservan protección contra abuso.
- Login, registro, checkout, órdenes y las demás operaciones mantienen el
  límite base; no se modifican autenticación, reglas de compra ni datos.
- CORS se procesa antes del limitador: los preflights no consumen cupo y un
  429 conserva las cabeceras CORS, `Retry-After` y `Cache-Control: no-store`.
- `Retry-After` redondea hacia arriba; una espera nunca termina antes de que
  se renueve el cupo.

La identificación del visitante requiere **activar la configuración de borde**.
El código puede desplegarse antes: con `RATE_LIMIT_EDGE_SECRET` vacío ignora
las cabeceras de identidad y mantiene el peer como respaldo, con los cupos
separados. Este estado intermedio no resuelve que distintos visitantes
compartan la identidad del proxy.

## Activación pendiente de aprobación manual en GCP

Proyecto: `destry-development`. No crear otro Gateway, balanceador o instancia.
No modificar SQL, datos, capacidad, facturación por solicitud ni conexiones.

1. El dueño mergea y despliega primero el PR de Ecommerce; después, el de
   Storefront. Confirmar CI y disponibilidad.
2. Guardar copias privadas de los dos backend services y de la revisión
   lista de Ecommerce, incluida su configuración de secretos.
3. Crear un secreto dedicado `selecto-rate-limit-edge-token`, con 32 bytes
   aleatorios codificados como hexadecimal y replicación automática. No
   reutilizar JWT, credenciales de pago ni secretos de webhooks.
4. Conceder `roles/secretmanager.secretAccessor` sobre ese secreto solamente
   a `destry-ecommerce-runtime@destry-development.iam.gserviceaccount.com`.
5. Actualizar `destry-ecommerce-staging` en `southamerica-east1` para enlazar
   `RATE_LIMIT_EDGE_SECRET` a una versión numérica de ese secreto. Conservar
   imagen y toda la configuración restante; esperar la revisión lista.
6. En los backend services globales **`destry-frontend-backend`** y
   **`destry-ecommerce-gateway-backend`**, agregar estas cabeceras de solicitud
   preservando cualquier cabecera existente:

   - `X-Selecto-Client-IP:{client_ip_address}`
   - `X-Selecto-Edge-Token:<valor privado del secreto>`

   El balanceador reemplaza las cabeceras homónimas enviadas por el cliente.
   El proxy Nginx conserva estas cabeceras tanto para `/api/` como para el
   subrequest interno `/_seo`. API Gateway normalmente reenvía las cabeceras.
   La aplicación exige el token correcto antes de aceptar una IP válida;
   normaliza IPv4/IPv6 y elimina ambas cabeceras antes del handler.

No ejecutar los pasos 3–6 sin aprobación del dueño. No guardar el token en
Git, salidas de comandos, capturas, archivos públicos ni cuerpos de PR.
El token también será visible a quienes puedan leer la configuración del
balanceador: es un secreto de esta frontera, no una credencial del negocio.

### Coste y reversión

Se reutiliza la infraestructura existente. Una versión con replicación
automática cuesta hasta **USD 0,06/mes**, más **USD 0,03 por 10.000 accesos**
fuera de las franquicias de Secret Manager. La lectura ocurre al iniciar
instancias, no por cada petición del cliente. Las franquicias se comparten
con la cuenta de facturación; no se presupone que sigan disponibles.
Las solicitudes y el tráfico mantienen sus cargos habituales.

Para revertir, restaurar las listas originales de cabeceras de ambos backend
services y retirar únicamente el enlace `RATE_LIMIT_EDGE_SECRET` o volver a
la revisión guardada. Se vuelve al respaldo por peer. No deshabilitar una
versión del secreto mientras una revisión activa la siga utilizando.

## Verificación antes de darlo por activado

- Abrir home y producto desde dos conexiones diferentes. Consultar los logs
  correlacionados por `request_id`: `verified_edge_client=true` y políticas
  `catalog`, `media` y `metadata`. No registrar el token ni cabeceras completas.
- Confirmar por una solicitud del navegador que el subrequest SEO también
  recibe identidad verificada. Si no, corregir el reenvío antes del cierre.
- Comprobar los preflights de Admin y que las solicitudes protegidas sin
  autenticación siguen respondiendo 401/403. No crear compras de prueba ni
  enviar correos o webhooks reales para comprobar el limitador.
- Los ensayos de ráfagas y falsificación se ejecutan en tests locales/CI,
  **nunca como prueba de carga contra producción**.
- El navegador hace como máximo un reintento automático de consultas cuando
  recibe un 429 con espera válida de hasta cinco minutos; respeta esa espera.
  No repite automáticamente mutaciones ni pagos.

Referencias oficiales:

- [Cabeceras personalizadas del balanceador](https://docs.cloud.google.com/load-balancing/docs/https/custom-headers-global).
- [Reenvío de cabeceras en API Gateway](https://docs.cloud.google.com/api-gateway/docs/authenticating-users-jwt).
- [Precios de Secret Manager](https://cloud.google.com/secret-manager/pricing).
