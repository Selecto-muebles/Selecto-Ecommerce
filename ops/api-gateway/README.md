# API Gateway de Ecommerce durante el corte a Selecto

`openapi.json` conserva las 47 rutas de la configuración productiva de Destry y
agrega `GET`/`OPTIONS /admin/payments/records/{record_id}`. El backend continúa
siendo el Cloud Run privado `destry-ecommerce-staging`; no se crea otro Gateway,
servicio, red ni balanceador.

La activación queda deliberadamente fuera de GitHub Actions. Requiere un operador
autenticado, comparación de rutas y una ventana de smoke controlada.

## Recursos fijos

- Proyecto: `destry-development`.
- API: `destry-ecommerce-staging-api`.
- Gateway: `destry-ecommerce-staging-gw` en `us-east1`.
- Cuenta de backend: `destry-gateway@destry-development.iam.gserviceaccount.com`.
- Configuración de rollback: `ecommerce-marketing-5d253a9-v4`.

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

Antes de actualizar se debe comprobar que la configuración compilada conserva
las 48 rutas. Después se prueban `/health`, catálogo, autenticación, CORS,
newsletter, comunicaciones y detalle multiproveedor del Backoffice.

## Rollback

```bash
gcloud api-gateway gateways update destry-ecommerce-staging-gw \
  --api=destry-ecommerce-staging-api \
  --api-config=ecommerce-marketing-5d253a9-v4 \
  --location=us-east1 \
  --project=destry-development
```

Las configuraciones anteriores no se eliminan.
