import { resolve } from 'node:path';

export class ApiError extends Error {
  constructor(code, status = 400) { super(code); this.status = status; }
}

export const openid = /^[A-Za-z0-9_-]{1,128}$/;
export const alias = /^[A-Za-z0-9_-]{1,64}$/;
export const secret = /^[\x21-\x7e]{32,1024}$/;

export function settings(env = process.env) {
  const users = list(env.QQ_ALLOWED_USERS);
  const groups = list(env.QQ_ALLOWED_GROUPS);
  if (!/^\d{1,32}$/.test(env.QQ_APP_ID ?? '') || !env.QQ_APP_SECRET) throw new Error('QQ credentials required');
  if (!secret.test(env.BOT_API_TOKEN ?? '')) throw new Error('BOT_API_TOKEN requires 32+ printable ASCII characters');
  const commandUrl = env.ENHANCEMENT_COMMAND_URL ?? '';
  const commandSecret = env.ENHANCEMENT_COMMAND_SECRET ?? '';
  if (commandUrl || commandSecret) {
    let parsed;
    try { parsed = new URL(commandUrl); } catch { throw new Error('Invalid ENHANCEMENT_COMMAND_URL'); }
    if (!['http:', 'https:'].includes(parsed.protocol) || parsed.username || parsed.password || parsed.hash) {
      throw new Error('Invalid ENHANCEMENT_COMMAND_URL');
    }
    if (!secret.test(commandSecret) || commandSecret === env.BOT_API_TOKEN || !users.length) {
      throw new Error('Commands require a separate secret and explicit QQ_ALLOWED_USERS');
    }
  }
  const port = Number(env.BOT_PORT ?? 8080);
  if (!Number.isInteger(port) || port < 0 || port > 65535) throw new Error('Invalid BOT_PORT');
  return {
    appId: env.QQ_APP_ID, appSecret: env.QQ_APP_SECRET, apiToken: env.BOT_API_TOKEN,
    dataDir: resolve(env.BOT_DATA_DIR || './data'), host: env.BOT_HOST || '127.0.0.1', port,
    commandUrl, commandSecret, users, groups,
  };
}

function list(value = '') {
  const entries = [...new Set(value.split(/[\s,]+/).filter(Boolean))];
  if (entries.length > 20 || entries.some(item => !openid.test(item))) throw new Error('Invalid QQ allowlist');
  return entries;
}

export function text(value, max = 1800) {
  let output = String(value).replace(/<[^>]*>/g, '').replace(/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/g, '').trim();
  if (Buffer.byteLength(output) <= max) return output;
  let bytes = 0;
  output = Array.from(output).filter(char => (bytes += Buffer.byteLength(char)) <= max - 3).join('');
  return output + '…';
}
