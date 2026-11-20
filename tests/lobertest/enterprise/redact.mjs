export function redact(value, secrets = []) {
  let text = String(value)
    .replace(/access_token=[^&\s"']+/gi, 'access_token=[redacted]')
    .replace(/\bBearer\s+[^\s"',;\]}]+/gi, 'Bearer [redacted]');
  for (const secret of secrets.filter(Boolean)) {
    for (const encoded of new Set([secret, encodeURIComponent(secret)])) {
      text = text.split(encoded).join('[redacted]');
    }
  }
  return text;
}
