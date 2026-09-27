import assert from 'node:assert/strict';
import { createHmac, randomUUID, timingSafeEqual } from 'node:crypto';
import { assertLoopback, quote } from './safety.mjs';

export function signedHeaders(raw, secret) {
  const timestamp = String(Math.floor(Date.now() / 1000));
  return { 'X-Service-Name': 'selecto-payments', 'X-Service-Timestamp': timestamp,
    'X-Service-Signature': 'sha256=' + createHmac('sha256', secret).update(timestamp + '.' + raw).digest('hex') };
}
export function verifySigned(headers, raw, secret) {
  const timestamp = headers['x-service-timestamp'], provided = headers['x-service-signature'] || '';
  if (!timestamp || Math.abs(Date.now() / 1000 - Number(timestamp)) > 300) return false;
  const expected = 'sha256=' + createHmac('sha256', secret).update(timestamp + '.' + raw).digest('hex');
  return provided.length === expected.length && timingSafeEqual(Buffer.from(provided), Buffer.from(expected));
}
export async function runScenarios({ apiURL, secret, sql, password, payloads }) {
  assertLoopback(apiURL);
  const evidence = [];
  async function request(path, method = 'GET', body, token, headers = {}) {
    const response = await fetch(apiURL + path, { method, headers: { 'Content-Type': 'application/json', ...(token && { Authorization: `Bearer ${token}` }), ...headers },
      body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(15000) });
    const text = await response.text(); let data = null; try { data = JSON.parse(text); } catch { /* XML/empty */ }
    return { status: response.status, data, text };
  }
  const expectStatus = (response, expected, name) => { assert.equal(response.status, expected, name); return response.data; };
  const tokenFromOutbox = (email, template) => {
    const url = sql(`SELECT payload->>'url' FROM email_outbox WHERE recipient=${quote(email)} AND template=${quote(template)} ORDER BY id DESC LIMIT 1`);
    assert.ok(url, 'local verification/reset email was queued'); return new URL(url).searchParams.get('token');
  };
  async function customer(email) {
    expectStatus(await request('/register', 'POST', { email, password }), 200, 'minimal signup');
    expectStatus(await request('/login', 'POST', { email, password }), 403, 'unverified login denied');
    expectStatus(await request('/auth/email/verify', 'POST', { token: tokenFromOutbox(email, 'verify_email') }), 200, 'verify email');
    return expectStatus(await request('/login', 'POST', { email, password }), 200, 'normal customer login').token;
  }
  const email = 'customer@selecto-cert.test.invalid', otherEmail = 'other@selecto-cert.test.invalid';
  const token = await customer(email), otherToken = await customer(otherEmail);
  assert.equal(sql(`SELECT COALESCE(dni,'')||':'||COALESCE(street_address,'') FROM users WHERE email=${quote(email)}`), ':');
  evidence.push('signup + verification + normal login; no invented address/DNI');
  sql(`INSERT INTO users(email,password,role,email_verified_at) VALUES ('admin@selecto-cert.test.invalid',public.crypt(${quote(password)},public.gen_salt('bf',8)),'admin',NOW())`);
  const adminToken = expectStatus(await request('/login', 'POST', { email: 'admin@selecto-cert.test.invalid', password }), 200, 'normal admin login').token;
  expectStatus(await request('/admin/me', 'GET', undefined, token), 403, 'customer cannot impersonate admin');
  expectStatus(await request('/admin/me'), 401, 'anonymous admin denied');
  expectStatus(await request('/admin/me', 'GET', undefined, adminToken), 200, 'authenticated admin');
  const category = expectStatus(await request('/admin/categories', 'POST', { name: 'Laboratorio', slug: 'laboratorio', active: true }, adminToken), 201, 'category create');
  async function product(name, stock = 100) {
    return expectStatus(await request('/admin/products', 'POST', { name, sku: randomUUID(), price: 1000, stock, active: true, category: 'Laboratorio', description: 'Producto sintético exclusivo de certificación.' }, adminToken), 201, 'product create');
  }
  const item = await product('Equipo de certificación');
  const shipping = { first_name: 'Ana', last_name: 'Prueba', dni: '87654321', street_address: 'Calle de prueba', street_number: '100',
    postal_code: '1000', province: 'Buenos Aires', locality: 'CABA', phone_number: '1112345678',
    requested_delivery_date: new Date(Date.now() + 7 * 86400000).toISOString().slice(0, 10) };
  const orderInput = (productID, quantity) => ({ items: [{ product_id: productID, quantity }], shipping_address: shipping });
  const key = randomUUID(), input = orderInput(item.id, 3);
  const order = expectStatus(await request('/orders', 'POST', input, token, { 'Idempotency-Key': key }), 201, 'create order');
  assert.equal(order.total, 2850, 'three units receive 5% discount');
  const replay = expectStatus(await request('/orders', 'POST', input, token, { 'Idempotency-Key': key }), 200, 'idempotent replay');
  assert.equal(replay.order_id, order.order_id);
  expectStatus(await request('/orders', 'POST', orderInput(item.id, 4), token, { 'Idempotency-Key': key }), 409, 'changed idempotent request denied');
  expectStatus(await request(`/orders/${order.order_id}`, 'GET', undefined, otherToken), 404, 'another customer cannot read the order');
  const checkout = expectStatus(await request('/checkout', 'POST', { order_id: order.order_id }, token), 200, 'checkout minimal user');
  assert.ok(checkout.checkout_url.startsWith('http://127.0.0.1:'), 'checkout never leaves lab');
  assert.equal(payloads.length, 1); assert.equal(payloads[0].customer.identification, shipping.dni); assert.equal(payloads[0].customer.name, 'Ana Prueba');
  expectStatus(await request('/checkout', 'POST', { order_id: order.order_id }, token), 200, 'checkout replay');
  assert.equal(payloads.length, 1, 'no duplicate preference');
  evidence.push('shipping snapshot reaches Payments; discounts and preference/idempotency preserved');
  const event = { payment_provider: 'mobbex', provider_payment_id: 'synthetic-lab-payment', order_id: order.order_id, status: 'paid', amount: order.total };
  expectStatus(await request('/payments/webhook', 'POST', event), 401, 'unsigned webhook denied');
  const headers = signedHeaders(JSON.stringify(event), secret);
  expectStatus(await request('/payments/webhook', 'POST', event, undefined, headers), 200, 'signed simulated webhook');
  expectStatus(await request('/payments/webhook', 'POST', event, undefined, headers), 200, 'duplicate simulated webhook');
  assert.equal(sql("SELECT COUNT(*) FROM payment_webhook_events WHERE provider_payment_id='synthetic-lab-payment'"), '1');
  const paid = expectStatus(await request(`/orders/${order.order_id}`, 'GET', undefined, token), 200, 'paid order visible');
  assert.equal(paid.status, 'paid');
  evidence.push('signed simulated payment + duplicate callback; NOT a real Mobbex certification');
  expectStatus(await request(`/products/${item.id}/reviews`, 'POST', { rating: 5, title: 'Aceptación real del flujo', comment: 'Comentario sintético de certificación, no de un cliente real.' }, otherToken), 403, 'review without paid purchase denied');
  const review = expectStatus(await request(`/products/${item.id}/reviews`, 'POST', { rating: 5, title: 'Aceptación real del flujo', comment: 'Comentario sintético de certificación, no de un cliente real.' }, token), 201, 'verified paid buyer review');
  assert.equal(expectStatus(await request(`/products/${item.id}/reviews`), 200, 'pending review hidden').total, 0);
  expectStatus(await request(`/admin/product-reviews/${review.id}`, 'PATCH', { status: 'published', note: 'Prueba local' }, adminToken), 200, 'publish review');
  assert.equal(expectStatus(await request(`/products/${item.id}/reviews`), 200, 'public verified review').total, 1);
  expectStatus(await request(`/admin/product-reviews/${review.id}`, 'PATCH', { status: 'pending', note: 'Remoderación local' }, adminToken), 200, 'remoderation removes from public');
  assert.equal(expectStatus(await request(`/products/${item.id}/reviews`), 200, 'review withdrawn').total, 0);
  evidence.push('paid purchase required for review; pending/published/remoderated visibility');
  const limited = await product('Stock concurrente', 10);
  const concurrent = await Promise.all(Array.from({ length: 40 }, () => request('/orders', 'POST', orderInput(limited.id, 1), token, { 'Idempotency-Key': randomUUID() })));
  assert.equal(concurrent.filter(result => result.status === 201).length, 10, 'exactly available stock reserved');
  assert.ok(concurrent.every(result => [201, 409].includes(result.status)), 'unexpected concurrency failure');
  const current = expectStatus(await request(`/admin/products/${limited.id}`, 'GET', undefined, adminToken), 200, 'stock after race');
  assert.equal(current.stock, 0);
  const once = await product('Reserva única', 10), onceKey = randomUUID();
  const repeated = await Promise.all(Array.from({ length: 20 }, () => request('/orders', 'POST', orderInput(once.id, 1), token, { 'Idempotency-Key': onceKey })));
  assert.equal(repeated.filter(result => result.status === 201).length, 1);
  assert.ok(repeated.every(result => [200, 201].includes(result.status)));
  assert.equal(new Set(repeated.map(result => result.data.order_id)).size, 1);
  const onceOrder = repeated[0].data.order_id;
  const cancels = await Promise.all(Array.from({ length: 10 }, () => request(`/orders/${onceOrder}/cancel`, 'POST', {}, token)));
  assert.ok(cancels.every(result => [200, 409].includes(result.status)));
  const released = expectStatus(await request(`/admin/products/${once.id}`, 'GET', undefined, adminToken), 200, 'released stock');
  assert.equal(released.stock, 10, 'cancel releases once');
  evidence.push('40 parallel reservations: 10 successes, no oversell; 20 replayed requests: one order; concurrent cancel releases once');
  expectStatus(await request(`/admin/products/${once.id}/status`, 'PATCH', { active: false }, adminToken), 200, 'deactivate');
  const listed = expectStatus(await request('/admin/products', 'GET', undefined, adminToken), 200, 'inactive product remains in Admin');
  assert.ok(listed.items.some(product => product.id === once.id && !product.active));
  const disposable = await product('Sin historia');
  expectStatus(await request(`/admin/products/${disposable.id}`, 'DELETE', undefined, adminToken), 204, 'delete unreferenced product');
  expectStatus(await request(`/admin/products/${disposable.id}`, 'GET', undefined, adminToken), 404, 'deleted product absent');
  evidence.push('deactivation preserves Admin record; DELETE independently removes unreferenced product');
  expectStatus(await request('/auth/password/forgot', 'POST', { email: otherEmail }), 202, 'request password reset');
  expectStatus(await request('/auth/password/reset', 'POST', { token: tokenFromOutbox(otherEmail, 'password_reset'), password: password + '-new' }), 200, 'reset password');
  expectStatus(await request('/me', 'GET', undefined, otherToken), 401, 'reset revokes previous session');
  sql("UPDATE users SET role='user' WHERE email='admin@selecto-cert.test.invalid'");
  expectStatus(await request('/admin/me', 'GET', undefined, adminToken), 403, 'changed role immediately revokes admin permission');
  sql("UPDATE users SET role='admin' WHERE email='admin@selecto-cert.test.invalid'");
  evidence.push('IDOR, unsigned webhook, role changes and reset session revocation');
  const timings = [], statuses = {};
  let remaining = 500;
  await Promise.all(Array.from({ length: 20 }, async () => {
    while (remaining-- > 0) {
      const start = performance.now(); const response = await request('/products'); timings.push(performance.now() - start);
      statuses[response.status] = (statuses[response.status] || 0) + 1;
    }
  }));
  assert.equal(timings.length, 500); assert.deepEqual(statuses, { 200: 500 }); timings.sort((a, b) => a - b);
  const load = { target: 'loopback only; not production capacity', requests: 500, concurrency: 20, statuses,
    p50_ms: +timings[249].toFixed(2), p95_ms: +timings[474].toFixed(2), max_ms: +timings.at(-1).toFixed(2) };
  const head = await request('/seo/head?path=/productos/' + item.id);
  assert.equal(head.status, 200); assert.ok(head.text.includes('Equipo de certificación | Selecto'));
  const sitemap = await request('/sitemap.xml'); assert.equal(sitemap.status, 200); assert.ok(sitemap.text.includes('/productos/' + item.id));
  evidence.push('dynamic XML sitemap and server-generated social metadata');
  return { evidence, load, fixture: { email, password, productID: item.id, categoryID: category.id } };
}
