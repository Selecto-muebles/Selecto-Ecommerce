# API Gateway de Ecommerce durante el corte a Selecto

`openapi.json` contiene las 60 rutas vigentes de Ecommerce, incluidas categorías,
contenido editorial, reseñas y recuperación de acceso administrativo. Mantiene
los contratos existentes, incluidos `GET`/`OPTIONS
/admin/payments/records/{record_id}` y `DELETE /admin/products/{id}`. El backend
continúa siendo el Cloud Run privado
`destry-ecommerce-staging`; no se crea otro Gateway, servicio, red ni
balanceador.

La activación queda deliberadamente fuera de GitHub Actions. Requiere un operador
autenticado, comparación de rutas y una ventana de smoke controlada.

## Recursos fijos

- Proyecto: `destry-development`.
- API: `destry-ecommerce-staging-api`.
- Gateway: `destry-ecommerce-staging-gw` en `us-east1`.
- Cuenta de backend: `destry-gateway@destry-development.iam.gserviceaccount.com`.
- Configuración de rollback: la API Config activa capturada antes de cada corte.

## Activación segura

Desde este directorio, reemplazar `<config-id>` por un nombre inmutable que
incluya el SHA certificado:

```bash
gcloud api-gateway api-configs create <config-id> \
  --api=destry-ecommerce-staging-api \
  --openapi-spec=openapi.json \
  --backend-auth-service-account=destry-gateway@destry-development.iam.gserviceaccount.com \
  --project=destry-development

gcloud api-gateway gateways update destry-ecommerce-staging-gw \
  --api=destry-ecommerce-staging-api \
  --api-config=<config-id> \
  --location=us-east1 \
  --project=destry-development
```

Antes de actualizar se debe capturar el ID activo para rollback y comprobar que
la configuración compilada conserva las 60 rutas esperadas. Después se prueban
`/health`, catálogo, autenticación, CORS, recuperación administrativa,
eliminación protegida de productos, newsletter, comunicaciones y detalle
multiproveedor del Backoffice.

## Rollback

```bash
gcloud api-gateway gateways update destry-ecommerce-staging-gw \
  --api=destry-ecommerce-staging-api \
  --api-config=<config-id-anterior> \
  --location=us-east1 \
  --project=destry-development
```

Las configuraciones anteriores no se eliminan.
