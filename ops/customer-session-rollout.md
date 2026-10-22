# Despliegue de sesiones persistentes de clientes

La renovación de 30 días requiere **dos repositorios y una API Config nueva**. No
activar el frontend antes de comprobar que el Gateway expone los endpoints y
preserva `Cookie` / `Set-Cookie` a través del proxy `/api/` de Nginx. El JWT de acceso sigue venciendo como máximo
a las 24 horas; la cookie HttpOnly permite renovarlo sin guardar un refresh
token en JavaScript.

## Orden seguro

1. Revisar y mergear manualmente el PR de Ecommerce. Su migración 024 crea
   `customer_refresh_sessions`; no borra sesiones JWT existentes.
2. Desplegar Ecommerce y activar manualmente una API Config de Gateway basada
   en `ops/api-gateway/openapi.json`, conservando el ID anterior para rollback.
3. Con una cuenta de cliente de prueba, verificar desde
   `https://selectosport.com` que login/Google devuelven una cookie
   `__Host-selecto_refresh` con `Secure`, `HttpOnly`, `SameSite=Lax` y `Path=/`;
   que `POST /auth/session/refresh` entrega otro JWT y rota la cookie; y que
   `POST /auth/session/logout` la revoca. No copiar tokens ni cookies a logs.
4. Solo entonces mergear y desplegar manualmente el PR del frontend. Probar
   login manual y Google, cierre/reapertura del navegador, expiración de JWT,
   dos pestañas, cambio de cuenta, logout y checkout sin duplicar órdenes.

## Si algo falla

Revertir primero el frontend a la imagen anterior. Sus clientes siguen usando
JWT por pestaña y no dependen de los endpoints nuevos. Si hace falta, restaurar
la API Config anterior y la imagen previa de Ecommerce; dejar la tabla 024
intacta para evitar una operación destructiva en producción. Nunca restaurar
una copia de la base solo para revertir esta función.

La suite local no prueba que el Gateway productivo preserve cookies ni que la
base de producción migre correctamente; son verificaciones del corte, no
suposiciones.
