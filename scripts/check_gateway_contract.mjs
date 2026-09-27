import { readFileSync } from "node:fs";

const spec = JSON.parse(
  readFileSync(new URL("../ops/api-gateway/openapi.json", import.meta.url), "utf8"),
);

const expectedPathCount = 69;
const requiredMethods = new Map([
  ["/health", "get"],
  ["/seo/head", "get"],
  ["/sitemap.xml", "get"],
  ["/admin/categories/{id}", "patch"],
  ["/admin/categories/reorder", "patch"],
  ["/marketing/newsletter/unsubscribe/confirm", "post"],
  ["/communications/brevo/webhook", "post"],
  ["/admin/communications/brevo/events", "get"],
  ["/admin/communications/brevo/status", "get"],
  ["/admin/marketing/subscriptions/{id}/sync", "post"],
  ["/marketing/newsletter", "post"],
  ["/marketing/newsletter/unsubscribe", "post"],
  ["/admin/marketing/subscriptions", "get"],
  ["/admin/communications/outbox", "get"],
  ["/admin/payments/records/{record_id}", "get"],
  ["/products/{id}/reviews", "post"],
  ["/admin/product-reviews", "get"],
  ["/admin/product-reviews/{id}", "patch"],
  ["/admin/auth/password/forgot", "post"],
  ["/admin/auth/password/reset", "post"],
  ["/admin/products/{id}", "delete"],
]);

const errors = [];
const paths = spec.paths ?? {};
if (Object.keys(paths).length !== expectedPathCount) {
  errors.push(
    `expected ${expectedPathCount} paths, found ${Object.keys(paths).length}; review intentional additions or removals`,
  );
}

for (const [path, method] of requiredMethods) {
  if (!paths[path]?.[method]) errors.push(`missing ${method.toUpperCase()} ${path}`);
}
for (const [path, methods] of Object.entries(paths)) {
  if (!methods.options) errors.push(`missing OPTIONS ${path}`);
}

if (errors.length > 0) {
  console.error(`Gateway contract validation failed:\n- ${errors.join("\n- ")}`);
  process.exit(1);
}

console.log(`Gateway contract validated: ${expectedPathCount} paths with OPTIONS.`);
