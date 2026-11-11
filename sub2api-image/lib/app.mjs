import express from 'express';
import { DatabaseSync } from 'node:sqlite';
import { createHash, createHmac, randomBytes, randomUUID, scrypt, scryptSync, timingSafeEqual } from 'node:crypto';
import { mkdirSync } from 'node:fs';
import { unlink, writeFile } from 'node:fs/promises';
import { promisify } from 'node:util';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const rootDir = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const derivePassword = promisify(scrypt);
const maxImageBytes = 30 * 1024 * 1024;
const sessionLifetime = 7 * 24 * 60 * 60 * 1000;
const sizes = new Set(['auto', '1024x1024', '1536x1024', '1024x1536', '1792x1024', '1024x1792']);
const qualities = new Set(['auto', 'low', 'medium', 'high', 'standard', 'hd']);
const backgrounds = new Set(['auto', 'opaque', 'transparent']);
const limits = { guest: 3, registered: 10, paid: 100 };
const hashToken = token => createHash('sha256').update(token).digest('hex');

function httpError(status, message, extra = {}) {
  return Object.assign(new Error(message), { status, ...extra });
}

function normalizeBaseUrl(value) {
  if (typeof value !== 'string' || value.length > 2048) throw new Error('SUB2API_BASE_URL 必须是有效地址。');
  if (!value.trim()) return '';
  let url;
  try { url = new URL(value.trim()); } catch { throw new Error('SUB2API_BASE_URL 必须是完整的 HTTP 或 HTTPS 地址。'); }
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash) {
    throw new Error('SUB2API_BASE_URL 仅支持 HTTP / HTTPS，不能包含密码、查询参数或锚点。');
  }
  return url.href.replace(/\/+$/, '').replace(/\/v1$/, '');
}

function configuredBoolean(value) {
  return value === true || value === 1 || ['true', '1'].includes(String(value).toLowerCase());
}

function proxySetting(value) {
  if (value === undefined || value === '' || value === false || value === 0 || /^(false|0)$/i.test(String(value))) return false;
  if (value === true) return true;
  if (/^\d+$/.test(String(value))) return Number(value);
  // Express accepts an explicit comma-separated list of trusted IP addresses or subnets.
  return String(value).split(',').map(item => item.trim()).filter(Boolean);
}

function safeMessage(message, apiKey) {
  let result = String(message || '请求失败，请稍后重试。');
  if (apiKey) result = result.split(apiKey).join('[已隐藏密钥]');
  return result.replace(/Bearer\s+\S+/gi, 'Bearer [已隐藏]').replace(/sk-[\w-]{8,}/g, '[已隐藏密钥]').slice(0, 1000);
}

function sameBytes(left, right) {
  return left.length === right.length && timingSafeEqual(left, right);
}

function credentials(body) {
  if (!body || typeof body !== 'object' || Array.isArray(body)) throw httpError(400, '账号信息格式无效。');
  if (typeof body.username !== 'string' || !/^[A-Za-z0-9_]{3,32}$/.test(body.username)) {
    throw httpError(400, '用户名须为 3 至 32 位字母、数字或下划线。');
  }
  if (typeof body.password !== 'string' || body.password.length < 8 || body.password.length > 128) {
    throw httpError(400, '密码长度须为 8 至 128 字符。');
  }
  return { username: body.username.toLowerCase(), password: body.password };
}

function pagination(query, defaultLimit = 24) {
  const page = Number(query.page ?? 1);
  const limit = Number(query.limit ?? defaultLimit);
  if (!Number.isInteger(page) || page < 1 || !Number.isInteger(limit) || limit < 1 || limit > 100) {
    throw httpError(400, '分页参数无效。');
  }
  return { page, limit };
}

function cookieValue(req, name) {
  const parts = (req.get('cookie') || '').split(';');
  const value = parts.map(item => item.trim()).find(item => item.startsWith(`${name}=`))?.slice(name.length + 1);
  return typeof value === 'string' && /^[A-Za-z0-9_-]{43}$/.test(value) ? value : null;
}

async function readLimited(response, limit) {
  if (Number(response.headers.get('content-length')) > limit) {
    await response.body?.cancel();
    throw new Error('上游返回内容过大，请减少图片数量或尺寸。');
  }
  const chunks = [];
  let length = 0;
  for await (const chunk of response.body || []) {
    length += chunk.length;
    if (length > limit) throw new Error('上游返回内容过大，请减少图片数量或尺寸。');
    chunks.push(chunk);
  }
  return Buffer.concat(chunks, length);
}

async function readUpstreamJson(response) {
  const content = await readLimited(response, response.ok ? 180 * 1024 * 1024 : 64 * 1024);
  let body;
  try { body = JSON.parse(content.toString('utf8')); } catch {
    throw new Error(`上游返回了无法解析的内容（HTTP ${response.status}），请联系管理员检查 sub2api 地址。`);
  }
  if (!response.ok || body?.error) {
    const detail = typeof body?.error === 'string' ? body.error : body?.error?.message || body?.message;
    throw new Error(`上游请求失败（HTTP ${response.status}）${detail ? `：${detail}` : '，请联系管理员检查分组权限和模型。'}`);
  }
  return body;
}

function imageFormat(bytes) {
  if (bytes.length >= 24 && bytes.subarray(0, 8).equals(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]))) {
    return { ext: 'png', width: bytes.readUInt32BE(16), height: bytes.readUInt32BE(20) };
  }
  if (bytes.length >= 4 && bytes[0] === 255 && bytes[1] === 216 && bytes[2] === 255) return { ext: 'jpg' };
  if (bytes.length >= 12 && bytes.toString('ascii', 0, 4) === 'RIFF' && bytes.toString('ascii', 8, 12) === 'WEBP') return { ext: 'webp' };
  throw new Error('上游返回的内容不是有效的 PNG、JPEG 或 WebP 图片。');
}

export function createApp(options = {}) {
  const dataDir = path.resolve(options.dataDir || process.env.DATA_DIR || path.join(rootDir, 'data'));
  const imageDir = path.join(dataDir, 'images');
  mkdirSync(imageDir, { recursive: true });
  // Upstream credentials are deployment configuration. Never read legacy config.json.
  const config = {
    baseUrl: normalizeBaseUrl(options.baseUrl ?? process.env.SUB2API_BASE_URL ?? ''),
    apiKey: options.apiKey ?? process.env.SUB2API_API_KEY ?? '',
    model: options.model ?? process.env.SUB2API_MODEL ?? 'gpt-image-2',
    basicMode: configuredBoolean(options.basicMode ?? process.env.SUB2API_BASIC_MODE ?? false),
  };
  if (typeof config.model !== 'string' || !config.model.trim() || config.model.length > 200 || /[\x00-\x1f]/.test(config.model)) {
    throw new Error('SUB2API_MODEL 必须是有效的模型名称。');
  }
  config.model = config.model.trim();
  if (typeof config.apiKey !== 'string' || config.apiKey.length > 4096 || /[\r\n]/.test(config.apiKey)) throw new Error('SUB2API_API_KEY 格式无效。');
  const timeoutSeconds = Math.max(5, Math.min(1800, Number(options.timeoutSeconds ?? process.env.GENERATION_TIMEOUT_SECONDS) || 900));
  const nowMs = () => new Date(options.now ? options.now() : Date.now()).getTime();
  const isoNow = () => new Date(nowMs()).toISOString();
  const db = new DatabaseSync(path.join(dataDir, 'history.sqlite'));
  db.exec(`
    PRAGMA journal_mode = WAL;
    PRAGMA busy_timeout = 5000;
    PRAGMA foreign_keys = ON;
    CREATE TABLE IF NOT EXISTS generations (
      id TEXT PRIMARY KEY, prompt TEXT NOT NULL, model TEXT NOT NULL,
      size TEXT NOT NULL, quality TEXT NOT NULL, background TEXT NOT NULL,
      n INTEGER NOT NULL, basic_mode INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL, error TEXT,
      created_at TEXT NOT NULL, completed_at TEXT, images TEXT NOT NULL DEFAULT '[]',
      owner TEXT NOT NULL DEFAULT 'legacy', quota_subject TEXT, quota_day TEXT,
      quota_reserved INTEGER NOT NULL DEFAULT 0, quota_used INTEGER NOT NULL DEFAULT 0
    );
    CREATE INDEX IF NOT EXISTS generations_created_at ON generations(created_at DESC);
    CREATE TABLE IF NOT EXISTS users (
      id TEXT PRIMARY KEY, username TEXT NOT NULL COLLATE NOCASE UNIQUE,
      password_salt TEXT NOT NULL, password_hash TEXT NOT NULL,
      role TEXT NOT NULL DEFAULT 'user', tier TEXT NOT NULL DEFAULT 'registered', created_at TEXT NOT NULL
    );
    CREATE TABLE IF NOT EXISTS sessions (
      token_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      expires_at INTEGER NOT NULL
    );
    CREATE INDEX IF NOT EXISTS sessions_expires_at ON sessions(expires_at);
    CREATE TABLE IF NOT EXISTS daily_usage (
      subject TEXT NOT NULL, day TEXT NOT NULL, used INTEGER NOT NULL DEFAULT 0,
      reserved INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(subject, day)
    );
    CREATE TABLE IF NOT EXISTS app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
  `);
  const columns = new Set(db.prepare('PRAGMA table_info(generations)').all().map(item => item.name));
  for (const [name, definition] of Object.entries({
    basic_mode: 'INTEGER NOT NULL DEFAULT 0', owner: "TEXT NOT NULL DEFAULT 'legacy'",
    quota_subject: 'TEXT', quota_day: 'TEXT', quota_reserved: 'INTEGER NOT NULL DEFAULT 0', quota_used: 'INTEGER NOT NULL DEFAULT 0',
  })) if (!columns.has(name)) db.exec(`ALTER TABLE generations ADD COLUMN ${name} ${definition}`);
  db.exec('CREATE INDEX IF NOT EXISTS generations_owner ON generations(owner, created_at DESC)');
  let guestSalt = db.prepare("SELECT value FROM app_settings WHERE key='guest_salt'").get()?.value;
  if (!guestSalt) {
    guestSalt = randomBytes(32).toString('hex');
    db.prepare("INSERT INTO app_settings(key,value) VALUES('guest_salt',?)").run(guestSalt);
  }

  function transaction(action) {
    db.exec('BEGIN IMMEDIATE');
    try { const result = action(); db.exec('COMMIT'); return result; }
    catch (error) { db.exec('ROLLBACK'); throw error; }
  }

  const adminUsername = String(options.adminUsername ?? process.env.ADMIN_USERNAME ?? 'admin').toLowerCase();
  const adminPassword = options.adminPassword ?? process.env.ADMIN_PASSWORD ?? '';
  if (!/^[a-z0-9_]{3,32}$/.test(adminUsername)) throw new Error('ADMIN_USERNAME 须为 3 至 32 位字母、数字或下划线。');
  if (adminPassword) {
    if (typeof adminPassword !== 'string' || adminPassword.length < 8 || adminPassword.length > 128) throw new Error('ADMIN_PASSWORD 长度须为 8 至 128 字符。');
    const seedId = db.prepare("SELECT value FROM app_settings WHERE key='admin_seed_user_id'").get()?.value;
    const seed = seedId ? db.prepare('SELECT * FROM users WHERE id=?').get(seedId) : null;
    const collision = db.prepare('SELECT id FROM users WHERE username=?').get(adminUsername);
    if ((collision && collision.id !== seed?.id) || (seed && seed.role !== 'admin')) {
      db.close();
      throw new Error('管理员用户名与已有账号冲突。请为 ADMIN_USERNAME 选择其他名称；已有普通用户不会自动成为管理员。');
    }
    if (seed) {
      const unchanged = sameBytes(scryptSync(adminPassword, seed.password_salt, 64), Buffer.from(seed.password_hash, 'hex'));
      transaction(() => {
        if (!unchanged) {
          const salt = randomBytes(16).toString('hex');
          db.prepare('UPDATE users SET password_salt=?,password_hash=? WHERE id=?').run(salt, scryptSync(adminPassword, salt, 64).toString('hex'), seed.id);
          db.prepare('DELETE FROM sessions WHERE user_id=?').run(seed.id);
        }
        db.prepare("UPDATE users SET username=?,tier='paid' WHERE id=?").run(adminUsername, seed.id);
      });
    } else {
      const id = randomUUID();
      const salt = randomBytes(16).toString('hex');
      const hash = scryptSync(adminPassword, salt, 64).toString('hex');
      transaction(() => {
        db.prepare("INSERT INTO users(id,username,password_salt,password_hash,role,tier,created_at) VALUES(?,?,?,?,'admin','paid',?)")
          .run(id, adminUsername, salt, hash, isoNow());
        db.prepare("INSERT INTO app_settings(key,value) VALUES('admin_seed_user_id',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value").run(id);
      });
    }
  }

  function dayInfo() {
    const shifted = new Date(nowMs() + 8 * 60 * 60 * 1000);
    const day = shifted.toISOString().slice(0, 10);
    const next = new Date(`${day}T00:00:00+08:00`).getTime() + 24 * 60 * 60 * 1000;
    return { day, resetAt: new Date(next).toISOString() };
  }
  function quotaFor(subject, tier) {
    const { day, resetAt } = dayInfo();
    const usage = db.prepare('SELECT used,reserved FROM daily_usage WHERE subject=? AND day=?').get(subject, day) || { used: 0, reserved: 0 };
    const limit = limits[tier];
    return { tier, limit, used: usage.used, reserved: usage.reserved, remaining: Math.max(0, limit - usage.used - usage.reserved), day, resetAt, timeZone: 'Asia/Shanghai' };
  }
  function settleGeneration(id, status, error = null) {
    transaction(() => {
      const row = db.prepare('SELECT * FROM generations WHERE id=?').get(id);
      const imageCount = JSON.parse(row.images).length;
      if (row.quota_subject && row.quota_day && row.quota_reserved > 0) {
        db.prepare(`UPDATE daily_usage SET reserved=max(0,reserved-?),used=used+? WHERE subject=? AND day=?`)
          .run(row.quota_reserved, imageCount, row.quota_subject, row.quota_day);
      }
      db.prepare('UPDATE generations SET status=?,error=?,completed_at=?,quota_reserved=0,quota_used=? WHERE id=?')
        .run(status, error, isoNow(), imageCount, id);
    });
  }
  for (const row of db.prepare("SELECT id FROM generations WHERE status IN ('queued','generating')").all()) {
    settleGeneration(row.id, 'failed', '服务已重启，未完成的任务已停止；已保存图片会保留，未使用的预留额度已释放。');
  }

  const app = express();
  app.disable('x-powered-by');
  app.set('trust proxy', proxySetting(options.trustProxy ?? process.env.TRUST_PROXY));
  app.get('/healthz', (_req, res) => res.json({ status: 'ok' }));
  app.use((req, res, next) => {
    res.set('X-Content-Type-Options', 'nosniff');
    res.set('Referrer-Policy', 'same-origin');
    res.set('X-Frame-Options', 'DENY');
    res.set('Content-Security-Policy', "default-src 'self'; img-src 'self' blob: data:; style-src 'self'; script-src 'self'; font-src 'self'; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'");
    if (!['GET', 'HEAD', 'OPTIONS'].includes(req.method)) {
      const origin = req.get('origin');
      let sameOrigin = !origin;
      try { sameOrigin = !origin || new URL(origin).host === req.get('host'); } catch { sameOrigin = false; }
      if (!sameOrigin || req.get('sec-fetch-site') === 'cross-site') return res.status(403).json({ error: '请从本应用页面提交请求。' });
      const contentType = (req.get('content-type') || '').split(';', 1)[0].trim().toLowerCase();
      if (contentType !== 'application/json') return res.status(415).json({ error: '请求必须使用 application/json。' });
    }
    next();
  });
  app.use('/api', (_req, res, next) => { res.set('Cache-Control', 'no-store'); next(); });
  app.use(express.json({ limit: '100kb' }));
  const cookieOptions = req => ({ httpOnly: true, sameSite: 'lax', secure: req.secure, path: '/' });
  app.use((req, res, next) => {
    const isPage = req.path === '/' || req.path === '/index.html';
    if (!isPage && !req.path.startsWith('/api/') && !req.path.startsWith('/images/')) return next();
    // Shared asset caches must never receive a visitor identity cookie.
    if (isPage) res.set('Cache-Control', 'private, no-store');
    let guestToken = cookieValue(req, 'sub2api_guest');
    if (!guestToken) {
      guestToken = randomBytes(32).toString('base64url');
      res.set('Cache-Control', 'private, no-store');
      res.cookie('sub2api_guest', guestToken, { ...cookieOptions(req), maxAge: 365 * 24 * 60 * 60 * 1000 });
    }
    req.guestOwner = `guest:${hashToken(guestToken)}`;
    req.guestSubject = `guest:${createHmac('sha256', guestSalt).update(req.ip || req.socket.remoteAddress || 'unknown').digest('hex')}`;
    const token = cookieValue(req, 'sub2api_session');
    req.user = token ? db.prepare('SELECT users.* FROM sessions JOIN users ON users.id=sessions.user_id WHERE sessions.token_hash=? AND sessions.expires_at>?')
      .get(hashToken(token), nowMs()) || null : null;
    req.owner = req.user ? `user:${req.user.id}` : req.guestOwner;
    req.quotaSubject = req.user ? `user:${req.user.id}` : req.guestSubject;
    req.tier = req.user ? (req.user.role === 'admin' ? 'paid' : req.user.tier) : 'guest';
    next();
  });

  const publicUser = user => user ? { id: user.id, username: user.username, role: user.role, tier: user.role === 'admin' ? 'paid' : user.tier } : null;
  const me = req => ({ user: publicUser(req.user), quota: quotaFor(req.quotaSubject, req.tier) });
  function requireAdmin(req) {
    if (!req.user) throw httpError(401, '请先登录管理员账号。');
    if (req.user.role !== 'admin') throw httpError(403, '此操作需要管理员权限。');
  }
  function startSession(req, res, user) {
    const token = randomBytes(32).toString('base64url');
    const old = cookieValue(req, 'sub2api_session');
    if (old) db.prepare('DELETE FROM sessions WHERE token_hash=?').run(hashToken(old));
    db.prepare('DELETE FROM sessions WHERE expires_at<=?').run(nowMs());
    db.prepare('INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)').run(hashToken(token), user.id, nowMs() + sessionLifetime);
    res.cookie('sub2api_session', token, { ...cookieOptions(req), maxAge: sessionLifetime });
    req.user = user;
    req.owner = req.quotaSubject = `user:${user.id}`;
    req.tier = user.role === 'admin' ? 'paid' : user.tier;
  }

  app.get('/api/config', (_req, res) => res.json({ model: config.model, configured: Boolean(config.baseUrl && config.apiKey), timeoutSeconds, basicMode: config.basicMode, quotaTimezone: 'Asia/Shanghai' }));
  app.get('/api/me', (req, res) => res.json(me(req)));
  app.post('/api/auth/register', async (req, res) => {
    const { username, password } = credentials(req.body);
    if (username === adminUsername || db.prepare('SELECT id FROM users WHERE username=?').get(username)) throw httpError(409, '此用户名已被使用或保留，请换一个用户名。');
    const salt = randomBytes(16).toString('hex');
    const hash = (await derivePassword(password, salt, 64)).toString('hex');
    const id = randomUUID();
    try {
      db.prepare("INSERT INTO users(id,username,password_salt,password_hash,role,tier,created_at) VALUES(?,?,?,?,'user','registered',?)")
        .run(id, username, salt, hash, isoNow());
    } catch (error) {
      if (error.code?.startsWith('SQLITE_CONSTRAINT') || /UNIQUE constraint/i.test(error.message)) throw httpError(409, '此用户名已被使用，请换一个用户名。');
      throw error;
    }
    startSession(req, res, db.prepare('SELECT * FROM users WHERE id=?').get(id));
    res.status(201).json(me(req));
  });
  app.post('/api/auth/login', async (req, res) => {
    const { username, password } = credentials(req.body);
    const user = db.prepare('SELECT * FROM users WHERE username=?').get(username);
    // Still run scrypt for unknown users so a failed lookup does not skip password work.
    const derived = await derivePassword(password, user?.password_salt || guestSalt, 64);
    if (!user || !sameBytes(derived, Buffer.from(user.password_hash, 'hex'))) throw httpError(401, '用户名或密码不正确。');
    startSession(req, res, user);
    res.json(me(req));
  });
  app.post('/api/auth/logout', (req, res) => {
    const token = cookieValue(req, 'sub2api_session');
    if (token) db.prepare('DELETE FROM sessions WHERE token_hash=?').run(hashToken(token));
    res.clearCookie('sub2api_session', cookieOptions(req));
    req.user = null;
    req.owner = req.guestOwner;
    req.quotaSubject = req.guestSubject;
    req.tier = 'guest';
    res.json(me(req));
  });
  app.get('/api/admin/users', (req, res) => {
    requireAdmin(req);
    const { page, limit } = pagination(req.query, 20);
    const query = typeof req.query.q === 'string' ? req.query.q.trim().slice(0, 100) : '';
    const match = `%${query.replace(/[\\%_]/g, '\\$&')}%`;
    const total = db.prepare("SELECT count(*) AS n FROM users WHERE username LIKE ? ESCAPE '\\'").get(match).n;
    const items = db.prepare("SELECT * FROM users WHERE username LIKE ? ESCAPE '\\' ORDER BY created_at DESC, rowid DESC LIMIT ? OFFSET ?")
      .all(match, limit, (page - 1) * limit).map(user => ({ ...publicUser(user), createdAt: user.created_at, quota: quotaFor(`user:${user.id}`, user.role === 'admin' ? 'paid' : user.tier) }));
    res.json({ items, total, page, pages: Math.ceil(total / limit) });
  });
  app.patch('/api/admin/users/:id', (req, res) => {
    requireAdmin(req);
    const user = db.prepare('SELECT * FROM users WHERE id=?').get(req.params.id);
    if (!user) throw httpError(404, '账号不存在。');
    if (!['registered', 'paid'].includes(req.body?.tier)) throw httpError(400, '账号等级只能设为 registered 或 paid。');
    if (user.role === 'admin' && req.body.tier !== 'paid') throw httpError(400, '管理员账号固定使用每日 100 张额度。');
    db.prepare('UPDATE users SET tier=? WHERE id=?').run(req.body.tier, user.id);
    user.tier = req.body.tier;
    res.json({ user: { ...publicUser(user), createdAt: user.created_at }, quota: quotaFor(`user:${user.id}`, user.role === 'admin' ? 'paid' : user.tier) });
  });

  function generationFromRow(row) {
    if (!row) throw httpError(404, '未找到这条生成记录。');
    return { id: row.id, prompt: row.prompt, model: row.model, size: row.size, quality: row.quality,
      background: row.background, n: row.n, basicMode: Boolean(row.basic_mode), status: row.status, error: row.error,
      createdAt: row.created_at, completedAt: row.completed_at, images: JSON.parse(row.images),
    };
  }
  const getGeneration = id => generationFromRow(db.prepare('SELECT * FROM generations WHERE id=?').get(id));
  function ownerCondition(req) {
    return req.user?.role === 'admin' ? { sql: "(owner=? OR owner='legacy')", params: [req.owner] } : { sql: 'owner=?', params: [req.owner] };
  }
  function accessibleGeneration(req, id) {
    const owner = ownerCondition(req);
    return generationFromRow(db.prepare(`SELECT * FROM generations WHERE id=? AND ${owner.sql}`).get(id, ...owner.params));
  }
  const queue = [];
  let worker = null;
  let closed = false;
  let activeController = null;

  async function generate(id) {
    const generation = getGeneration(id);
    const controller = new AbortController();
    activeController = controller;
    const timer = setTimeout(() => controller.abort(), timeoutSeconds * 1000);
    const files = [];
    const images = [];
    try {
      db.prepare("UPDATE generations SET status='generating' WHERE id=?").run(id);
      const payload = generation.basicMode ? { prompt: generation.prompt, response_format: 'b64_json' }
        : { model: config.model, prompt: generation.prompt, n: generation.n, size: generation.size };
      if (!generation.basicMode) {
        if (generation.quality !== 'auto') payload.quality = generation.quality;
        if (generation.background !== 'auto') payload.background = generation.background;
        if (/^dall-e-/i.test(config.model)) payload.response_format = 'b64_json';
      }
      const response = await fetch(`${config.baseUrl}/v1/images/generations`, {
        method: 'POST', headers: { Authorization: `Bearer ${config.apiKey}`, 'Content-Type': 'application/json' },
        body: JSON.stringify(payload), signal: controller.signal, redirect: 'error',
      });
      const body = await readUpstreamJson(response);
      if (!Array.isArray(body?.data) || !body.data.length || body.data.length > 4) throw new Error('上游未返回有效图片列表（data），请联系管理员确认模型支持图片生成接口。');
      // Never expose more images than were reserved, even if the upstream returns extras.
      for (const item of body.data.slice(0, generation.n)) {
        if (!item || typeof item !== 'object') throw new Error('上游图片数据格式无效。');
        let bytes;
        if (typeof item.b64_json === 'string' && item.b64_json) {
          if (item.b64_json.length > Math.ceil(maxImageBytes / 3) * 4 + 4 || !/^[A-Za-z0-9+/\r\n]+={0,2}$/.test(item.b64_json)) throw new Error('图片 Base64 数据无效或超过 30 MB。');
          bytes = Buffer.from(item.b64_json, 'base64');
        } else if (typeof item.url === 'string') {
          const imageUrl = new URL(item.url);
          if (!['http:', 'https:'].includes(imageUrl.protocol) || imageUrl.username || imageUrl.password) throw new Error('上游图片下载地址无效。');
          const download = await fetch(imageUrl, { signal: controller.signal, redirect: 'follow' });
          if (!download.ok) throw new Error(`图片下载失败（HTTP ${download.status}）。`);
          bytes = await readLimited(download, maxImageBytes);
        } else throw new Error('上游图片缺少 b64_json 或 url。');
        if (bytes.length > maxImageBytes) throw new Error('单张图片超过 30 MB。');
        const { ext, ...dimensions } = imageFormat(bytes);
        const imageId = randomUUID();
        const filename = `${imageId}.${ext}`;
        const imagePath = path.join(imageDir, filename);
        files.push(imagePath);
        await writeFile(imagePath, bytes, { flag: 'wx' });
        images.push({ id: imageId, url: `/images/${filename}`, downloadUrl: `/api/images/${filename}/download`, ...dimensions,
          ...(typeof item.revised_prompt === 'string' ? { revisedPrompt: item.revised_prompt } : {}),
        });
        db.prepare('UPDATE generations SET images=? WHERE id=?').run(JSON.stringify(images), id);
      }
      settleGeneration(id, 'completed');
    } catch (error) {
      const retainedFiles = new Set(images.map(image => path.join(imageDir, path.basename(image.url))));
      await Promise.allSettled(files.filter(file => !retainedFiles.has(file)).map(file => unlink(file)));
      let message = controller.signal.aborted ? (closed ? '服务正在停止，任务已中断。' : '生图超时，请稍后检查上游状态再重试。') : safeMessage(error.message, config.apiKey);
      if (/capability|no available accounts|no available account|无可用账号|无可用账户/i.test(message)) message += ' 请联系管理员检查分组与模型权限，以及基础兼容模式配置。';
      if (images.length) message += ` 已保留 ${images.length} 张成功下载的图片，可在详情中预览和下载。`;
      settleGeneration(id, 'failed', message);
    } finally {
      clearTimeout(timer);
      activeController = null;
    }
  }
  function startWorker() {
    if (worker || closed) return;
    worker = Promise.resolve().then(async () => {
      while (queue.length && !closed) await generate(queue.shift());
    }).finally(() => { worker = null; });
  }

  app.post('/api/generations', (req, res) => {
    if (!config.baseUrl || !config.apiKey) throw httpError(503, '生图服务尚未配置，请联系管理员设置环境变量。');
    if (closed) throw httpError(503, '服务正在停止，请稍后再试。');
    if (queue.length + Number(Boolean(activeController)) >= 20) throw httpError(429, '当前待处理任务较多，请稍后再提交。', { quota: quotaFor(req.quotaSubject, req.tier) });
    const body = req.body;
    if (!body || typeof body !== 'object' || Array.isArray(body)) throw httpError(400, '生成参数格式无效。');
    if (typeof body.prompt !== 'string' || !body.prompt.trim() || body.prompt.length > 16000) throw httpError(400, '请输入提示词，最多 16000 字符。');
    const size = body.size ?? '1024x1024';
    const quality = body.quality ?? 'auto';
    const background = body.background ?? 'auto';
    const n = body.n ?? 1;
    if (!sizes.has(size) || !qualities.has(quality) || !backgrounds.has(background) || !Number.isInteger(n) || n < 1 || n > 4) throw httpError(400, '图片尺寸、质量、背景或数量不受支持。图片数量必须为 1 至 4。');
    if (config.basicMode && n !== 1) throw httpError(400, '基础兼容模式每次只能生成一张图片。');
    const id = randomUUID();
    transaction(() => {
      const quota = quotaFor(req.quotaSubject, req.tier);
      if (quota.remaining < n) throw httpError(429, '今日剩余额度不足，请明天再来，或登录、联系管理员升级账号。', { quota });
      db.prepare('INSERT INTO daily_usage(subject,day,used,reserved) VALUES(?,?,0,?) ON CONFLICT(subject,day) DO UPDATE SET reserved=reserved+excluded.reserved')
        .run(req.quotaSubject, quota.day, n);
      db.prepare('INSERT INTO generations(id,prompt,model,size,quality,background,n,basic_mode,status,created_at,owner,quota_subject,quota_day,quota_reserved) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)')
        .run(id, body.prompt.trim(), config.model, size, quality, background, n, Number(config.basicMode), 'queued', isoNow(), req.owner, req.quotaSubject, quota.day, n);
    });
    const generation = getGeneration(id);
    queue.push(id);
    startWorker();
    res.status(202).json({ generation, quota: quotaFor(req.quotaSubject, req.tier) });
  });

  app.get('/api/generations', (req, res) => {
    const query = typeof req.query.q === 'string' ? req.query.q.trim().slice(0, 500) : '';
    const status = req.query.status ?? 'all';
    if (!['all', 'completed', 'failed', 'processing'].includes(status)) throw httpError(400, '历史记录状态筛选无效。');
    const { page, limit } = pagination(req.query);
    const owner = ownerCondition(req);
    let condition = `WHERE ${owner.sql}`;
    const params = [...owner.params];
    if (query) {
      condition += " AND (prompt LIKE ? ESCAPE '\\' OR model LIKE ? ESCAPE '\\')";
      const match = `%${query.replace(/[\\%_]/g, '\\$&')}%`;
      params.push(match, match);
    }
    if (status === 'processing') condition += " AND status IN ('queued','generating')";
    else if (status !== 'all') { condition += ' AND status=?'; params.push(status); }
    const total = db.prepare(`SELECT count(*) AS n FROM generations ${condition}`).get(...params).n;
    const items = db.prepare(`SELECT * FROM generations ${condition} ORDER BY created_at DESC,rowid DESC LIMIT ? OFFSET ?`).all(...params, limit, (page - 1) * limit).map(generationFromRow);
    const counts = db.prepare(`SELECT status,count(*) AS n FROM generations WHERE ${owner.sql} GROUP BY status`).all(...owner.params);
    const stats = { total: 0, completed: 0, processing: 0, failed: 0, images: 0 };
    for (const item of counts) { stats.total += item.n; stats[['queued', 'generating'].includes(item.status) ? 'processing' : item.status] += item.n; }
    stats.images = db.prepare(`SELECT coalesce(sum(json_array_length(images)),0) AS n FROM generations WHERE ${owner.sql}`).get(...owner.params).n;
    res.json({ items, total, page, pages: Math.ceil(total / limit), stats });
  });
  app.get('/api/generations/:id', (req, res) => res.json({ generation: accessibleGeneration(req, req.params.id) }));
  app.delete('/api/generations/:id', async (req, res) => {
    const generation = accessibleGeneration(req, req.params.id);
    if (['queued', 'generating'].includes(generation.status)) throw httpError(409, '任务正在处理中，完成后才能删除。');
    for (const image of generation.images) {
      try { await unlink(path.join(imageDir, path.basename(image.url))); } catch (error) { if (error.code !== 'ENOENT') throw error; }
    }
    db.prepare('DELETE FROM generations WHERE id=?').run(generation.id);
    res.status(204).end();
  });
  function requireImageOwner(req, filename) {
    if (!/^[\da-f-]{36}\.(png|jpg|webp)$/.test(filename)) throw httpError(404, '图片不存在。');
    const owner = ownerCondition(req);
    const row = db.prepare(`SELECT id FROM generations WHERE ${owner.sql} AND EXISTS(SELECT 1 FROM json_each(generations.images) WHERE json_extract(json_each.value,'$.url')=?) LIMIT 1`)
      .get(...owner.params, `/images/${filename}`);
    if (!row) throw httpError(404, '图片不存在。');
  }
  app.get('/images/:filename', (req, res) => {
    requireImageOwner(req, req.params.filename);
    res.set('Cache-Control', 'private, no-store');
    res.sendFile(req.params.filename, { root: imageDir }, error => {
      if (error && !res.headersSent) res.status(error.statusCode || 500).json({ error: '图片不存在或暂时无法预览。' });
    });
  });
  app.get('/api/images/:filename/download', (req, res) => {
    requireImageOwner(req, req.params.filename);
    res.download(path.join(imageDir, req.params.filename), `sub2api-${req.params.filename}`, error => {
      if (error && !res.headersSent) res.status(error.statusCode || 500).json({ error: '图片不存在或暂时无法下载。' });
    });
  });
  app.use('/vendor/phosphor', express.static(path.join(rootDir, 'node_modules/@phosphor-icons/web/src'), { index: false, dotfiles: 'deny', maxAge: '7d' }));
  app.use(express.static(path.join(rootDir, 'public'), { index: 'index.html', dotfiles: 'deny' }));
  app.use((_req, res) => res.status(404).json({ error: '页面或接口不存在。' }));
  app.use((error, _req, res, _next) => {
    const status = error.type === 'entity.parse.failed' ? 400 : error.status || 500;
    const message = error.type === 'entity.parse.failed' ? '请求 JSON 格式无效。'
      : status >= 500 && ![502, 503].includes(status) ? '服务器处理失败，请联系管理员检查数据目录权限和磁盘空间。' : safeMessage(error.message, config.apiKey);
    if (!res.headersSent) res.status(status).json({ error: message, ...(error.quota ? { quota: error.quota } : {}) });
  });

  let dbClosed = false;
  return {
    app,
    async close() {
      if (dbClosed) return;
      closed = true;
      activeController?.abort();
      if (worker) await worker;
      for (const id of queue) settleGeneration(id, 'failed', '服务正在停止，任务尚未开始，预留额度已释放。');
      queue.length = 0;
      db.close();
      dbClosed = true;
    },
  };
}
