// Safe, anonymous smoke test: never sends an order or a Gestionar credential.
const rawURL = process.env.SELECTO_API_URL;
if (!rawURL) throw new Error("Definí SELECTO_API_URL con el origen HTTPS de la API.");

const baseURL = new URL(rawURL);
if (
  baseURL.protocol !== "https:" ||
  !baseURL.hostname ||
  baseURL.username ||
  baseURL.password ||
  baseURL.pathname !== "/" ||
  baseURL.search ||
  baseURL.hash
) {
  throw new Error("SELECTO_API_URL debe ser un origen HTTPS sin credenciales ni ruta.");
}

const origin = "https://admin.selectosport.com";
const paths = [
  "/admin/integrations/gestionar/status",
  "/admin/integrations/gestionar/pickup-times",
];

for (const path of paths) {
  const url = new URL(path, baseURL);
  const response = await fetch(url, {
    redirect: "manual",
    signal: AbortSignal.timeout(10000),
  });
  if (response.status !== 401) {
    throw new Error(`GET ${path}: esperado 401 (ruta protegida), recibido ${response.status}.`);
  }

  const preflight = await fetch(url, {
    method: "OPTIONS",
    headers: {
      Origin: origin,
      "Access-Control-Request-Method": "GET",
      "Access-Control-Request-Headers": "authorization,x-request-id,x-correlation-id",
    },
    redirect: "manual",
    signal: AbortSignal.timeout(10000),
  });
  const allowedOrigin = preflight.headers.get("access-control-allow-origin");
  const allowedMethods = preflight.headers.get("access-control-allow-methods") ?? "";
  const allowedHeaders = (preflight.headers.get("access-control-allow-headers") ?? "").toLowerCase();
  if (
    preflight.status !== 204 ||
    allowedOrigin !== origin ||
    !allowedMethods.includes("GET") ||
    !allowedHeaders.includes("authorization")
  ) {
    throw new Error(`OPTIONS ${path}: preflight CORS inválido (${preflight.status}).`);
  }
  console.log(`${path}: GET protegido y preflight CORS correctos.`);
}
