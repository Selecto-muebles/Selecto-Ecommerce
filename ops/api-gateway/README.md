# API Gateway de Ecommerce durante el corte a Selecto

`openapi.json` contiene las 77 rutas vigentes de Ecommerce, incluidas categorías,
contenido editorial, reseñas, recuperación de acceso administrativo, términos
públicos, arrepentimiento y edición versionada de artículos legales. Mantiene
los contratos existentes, incluidos `GET`/`OPTIONS
/admin/payments/records/{record_id}` y `DELETE /admin/products/{id}`. El backend
continúa siendo el Cloud Run privado
`destry-ecommerce-staging`; no se crea otro Gateway, servicio, red ni
balanceador.

La activación queda deliberadamente fuera de GitHub Actions. Requiere un operador
autenticado, comparación de rutas y una ventana de smoke controlada. Desplegar
solo Ecommerce no activa rutas nuevas: API Gateway las rechaza con 404 antes de
que la petición llegue a Cloud Run.

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
la configuración compilada conserva las 77 rutas esperadas. Después se prueban
`/health`, catálogo, autenticación, CORS, recuperación administrativa,
eliminación protegida de productos, newsletter, comunicaciones y detalle
multiproveedor del Backoffice. También se comprueba que
`GET https://api.selectosport.com/legal/terms` responde 200 con versión y
`body_markdown`, y que el panel admin puede cargar el documento y la bandeja.

## Rollback

```bash
gcloud api-gateway gateways update destry-ecommerce-staging-gw \
  --api=destry-ecommerce-staging-api \
  --api-config=<config-id-anterior> \
  --location=us-east1 \
  --project=destry-development
```

Las configuraciones anteriores no se eliminan.
