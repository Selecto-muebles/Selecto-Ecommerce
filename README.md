# Selecto Ecommerce

API principal de Selecto para identidad, catalogo, stock, ordenes, checkout, clientes y administracion.

## Desarrollo local

```bash
cp .env.example .env
go run ./cmd/api
```

Validacion:

```bash
go test ./...
go test -race ./...
go vet ./...
```

## Configuracion

Todas las credenciales se reciben por variables de entorno y `.env.example` contiene solamente placeholders. En GCP se vinculan desde Secret Manager; ningún valor secreto se copia al repositorio.

## Comunicaciones y pagos

- `POST /marketing/newsletter` y `POST /marketing/newsletter/unsubscribe` administran consentimiento comercial.
- `GET /admin/marketing/subscriptions` y `GET /admin/communications/outbox` permiten operar suscripciones y correo transaccional.
- El contrato interno de pagos admite la identidad numérica heredada y la identidad externa `payment_provider/provider_payment_id`.

## Migración controlada

La transición desde Destry reutiliza los recursos aprobados en lugar de duplicarlos. La base conserva esquemas, identificadores, usuarios, roles y versiones de sesión; la continuidad de tokens requiere mantener el secreto JWT durante el corte. Las referencias de infraestructura y sus permisos se verifican por separado antes de promover una imagen.
