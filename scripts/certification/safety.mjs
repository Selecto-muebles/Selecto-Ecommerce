export function assertLoopback(raw) {
  const url = new URL(raw);
  if (url.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname) || url.username || url.password) {
    throw new Error('Certification refuses non-loopback targets');
  }
  return url;
}
export function assertOwnedContainer(name, owner, expectedOwner) {
  if (!/^selecto-cert-[a-f0-9]{16}$/.test(name) || owner !== expectedOwner || !/^[a-f0-9]{16}$/.test(expectedOwner)) {
    throw new Error('Certification refuses cleanup of unowned containers');
  }
}
export const quote = value => "'" + String(value).replaceAll("'", "''") + "'";
