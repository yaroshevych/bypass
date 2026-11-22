function decodeBase32(secret) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  const normalized = String(secret).toUpperCase().replace(/\s/g, '');
  const [, body, padding] = /^([A-Z2-7]+)(=*)$/.exec(normalized) ?? [];
  if (!body || ![0, 2, 4, 5, 7].includes(body.length % 8)
      || (padding && padding.length !== (8 - body.length % 8) % 8)) {
    throw new Error('Invalid Base32 secret');
  }
  const bytes = new Uint8Array(Math.floor(body.length * 5 / 8));
  let bits = 0;
  let buffer = 0;
  let offset = 0;
  for (const character of body) {
    buffer = (buffer << 5) | alphabet.indexOf(character);
    bits += 5;
    if (bits >= 8) {
      bits -= 8;
      bytes[offset++] = buffer >> bits;
      buffer &= (1 << bits) - 1;
    }
  }
  if (buffer !== 0) {
    bytes.fill(0);
    throw new Error('Invalid Base32 padding bits');
  }
  return bytes;
}

export function parseTotpSettings({ uri = '', secret = '', digits = '', period = '', algorithm = '' } = {}) {
  uri = String(uri).trim();
  if (uri) {
    const url = new URL(uri);
    if (url.protocol !== 'otpauth:' || url.hostname.toLowerCase() !== 'totp') {
      throw new Error('Invalid TOTP URI');
    }
    secret = url.searchParams.get('secret') ?? '';
    digits = url.searchParams.get('digits') ?? '';
    period = url.searchParams.get('period') ?? '';
    algorithm = url.searchParams.get('algorithm') ?? '';
  }
  digits = String(digits).trim() || '6';
  period = String(period).trim() || '30';
  const hash = /^(?:HMAC-)?SHA-?(1|256|512)$/i.exec(String(algorithm).trim() || 'SHA1');
  if (!/^[6-8]$/.test(digits) || !/^\d+$/.test(period)
      || !Number.isSafeInteger(Number(period) * 1000) || Number(period) < 1 || !hash) {
    throw new Error('Invalid TOTP settings');
  }
  return { secret: decodeBase32(secret), digits: Number(digits), period: Number(period), hash: 'SHA-' + hash[1] };
}

export async function generateOtp(key, counter, digits) {
  const message = new DataView(new ArrayBuffer(8));
  message.setBigUint64(0, BigInt(counter));
  const signature = new DataView(await crypto.subtle.sign('HMAC', key, message));
  const offset = signature.getUint8(signature.byteLength - 1) & 15;
  const value = signature.getUint32(offset) & 0x7fffffff;
  return String(value % 10 ** digits).padStart(digits, '0');
}
