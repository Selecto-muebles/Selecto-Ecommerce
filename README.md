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

Todas las credenciales se reciben por variables de entorno. `.env.example` contiene solamente placeholders; Selecto debe utilizar bases, JWT, HMAC, Google OAuth, SMTP y Mercado Pago independientes.

## Baseline

El codigo parte de una baseline funcional congelada como `v1.0.0-rc1`. No se trasladaron secretos, datos, catalogo ni certificaciones productivas: la evidencia de seguridad, carga y operacion debe generarse nuevamente para Selecto.
