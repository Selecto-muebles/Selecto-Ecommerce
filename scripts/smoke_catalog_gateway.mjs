// Read-only checks: OPTIONS and unauthenticated GET never change catalogue data.
const base = (process.env.CATALOG_API_URL ?? "https://api.selectosport.com").replace(/\/$/, "");
const origin = process.env.CATALOG_ADMIN_ORIGIN ?? "https://admin.selectosport.com";
const routes = [
  ["/admin/product-families", ["GET", "POST"]],
  ["/admin/products", ["GET", "POST"]],
  ["/admin/products/catalog-probe", ["GET", "PATCH", "DELETE"]],
  ["/admin/products/catalog-probe/status", ["PATCH"]],
  ["/admin/products/catalog-probe/stock-adjustments", ["POST"]],
  ["/admin/products/catalog-probe/images", ["POST"]],
  ["/admin/products/catalog-probe/images/image-probe", ["PATCH", "DELETE"]],
  ["/admin/categories", ["GET", "POST"]],
  ["/admin/categories/catalog-probe", ["PATCH", "DELETE"]],
  ["/admin/categories/reorder", ["PATCH"]],
];
const failures = [];
for (const [path, methods] of routes) {
  for (const method of methods) {
    try {
      const response = await fetch(`${base}${path}`, {
        method: "OPTIONS",
        headers: {
          Origin: origin,
          "Access-Control-Request-Method": method,
          "Access-Control-Request-Headers":
            "authorization,content-type,x-request-id,x-correlation-id",
        },
        signal: AbortSignal.timeout(15000),
      });
      const allowed = response.headers
        .get("access-control-allow-methods")
        ?.split(",")
        .map((value) => value.trim());
      if (
        response.status !== 204 ||
        response.headers.get("access-control-allow-origin") !== origin ||
        !allowed?.includes(method)
      ) {
        failures.push(
          `${method} ${path}: preflight ${response.status}, origin ${response.headers.get("access-control-allow-origin")}`,
        );
      }
      await response.arrayBuffer();
    } catch (error) {
      failures.push(`${method} ${path}: ${error.message}`);
    }
  }
}
for (const path of ["/admin/product-families", "/admin/products", "/admin/categories"]) {
  try {
    const response = await fetch(`${base}${path}`, {
      headers: { Origin: origin },
      signal: AbortSignal.timeout(15000),
    });
    if (response.status !== 401 || response.headers.get("access-control-allow-origin") !== origin)
      failures.push(`GET ${path}: expected protected 401 with CORS; found ${response.status}`);
    await response.arrayBuffer();
  } catch (error) {
    failures.push(`GET ${path}: ${error.message}`);
  }
}
if (failures.length) {
  console.error(
    `Catalog Gateway validation failed:\n${failures.map((value) => `- ${value}`).join("\n")}`,
  );
  process.exitCode = 1;
} else
  console.log(
    `Catalog Gateway verified: ${routes.length} routes, ${routes.reduce((count, [, methods]) => count + methods.length, 0)} preflights, 3 protected GETs.`,
  );
