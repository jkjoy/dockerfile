import express from 'express';
import { DatabaseSync } from 'node:sqlite';
import { randomUUID, timingSafeEqual } from 'node:crypto';
import { existsSync, mkdirSync, readFileSync, renameSync, writeFileSync } from 'node:fs';
import { unlink, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const rootDir = path.dirname(fileURLToPath(import.meta.url));
const maxImageBytes = 30 * 1024 * 1024;
const sizes = new Set(['auto', '1024x1024', '1536x1024', '1024x1536', '1792x1024', '1024x1792']);
const qualities = new Set(['auto', 'low', 'medium', 'high', 'standard', 'hd']);
const backgrounds = new Set(['auto', 'opaque', 'transparent']);

function httpError(status, message) {
  return Object.assign(new Error(message), { status });
}

function normalizeBaseUrl(value) {
  if (typeof value !== 'string' || value.length > 2048) throw httpError(400, '请填写有效的 sub2api 地址。');
  if (!value.trim()) return '';
  let url;
  try { url = new URL(value.trim()); } catch { throw httpError(400, 'sub2api 地址必须是完整的 HTTP 或 HTTPS 地址。'); }
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash) {
    throw httpError(400, 'sub2api 地址仅支持 HTTP / HTTPS，不能包含密码、查询参数或锚点。');
  }
  return url.href.replace(/\/+$/, '').replace(/\/v1$/, '');
}

function validateModel(value) {
  if (typeof value !== 'string' || !value.trim() || value.length > 200 || /[\r\n\x00-\x1f]/.test(value)) {
    throw httpError(400, '请填写有效的模型名称（最多 200 字符）。');
  }
  return value.trim();
}

function safeMessage(message, apiKey) {
  let result = String(message || '请求失败，请稍后重试。');
  if (apiKey) result = result.split(apiKey).join('[已隐藏密钥]');
  return result.replace(/Bearer\s+\S+/gi, 'Bearer [已隐藏]').replace(/sk-[\w-]{8,}/g, '[已隐藏密钥]').slice(0, 1000);
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
    throw new Error(`上游返回了无法解析的内容（HTTP ${response.status}），请检查地址是否为 sub2api API 地址。`);
  }
  if (!response.ok || body?.error) {
    const detail = typeof body?.error === 'string' ? body.error : body?.error?.message || body?.message;
    throw new Error(`上游请求失败（HTTP ${response.status}）${detail ? `：${detail}` : '，请检查 API Key、分组权限和模型。'}`);
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

function constantEquals(left, right) {
  const a = Buffer.from(left);
  const b = Buffer.from(right);
  return a.length === b.length && timingSafeEqual(a, b);
}

export function createApp(options = {}) {
  const dataDir = path.resolve(options.dataDir || process.env.DATA_DIR || path.join(rootDir, 'data'));
  const imageDir = path.join(dataDir, 'images');
  mkdirSync(imageDir, { recursive: true });
  const configFile = path.join(dataDir, 'config.json');
  const saved = existsSync(configFile) ? JSON.parse(readFileSync(configFile, 'utf8')) : {};
  let config = {
    baseUrl: normalizeBaseUrl(saved.baseUrl ?? options.baseUrl ?? process.env.SUB2API_BASE_URL ?? ''),
    apiKey: saved.apiKey ?? options.apiKey ?? process.env.SUB2API_API_KEY ?? '',
    model: validateModel(saved.model ?? options.model ?? process.env.SUB2API_MODEL ?? 'gpt-image-2'),
  };
  const timeoutSeconds = Math.max(5, Math.min(1800, Number(options.timeoutSeconds ?? process.env.GENERATION_TIMEOUT_SECONDS) || 900));
  const authUsername = options.authUsername ?? process.env.AUTH_USERNAME ?? 'admin';
  const authPassword = options.authPassword ?? process.env.AUTH_PASSWORD ?? '';
  const db = new DatabaseSync(path.join(dataDir, 'history.sqlite'));
  db.exec(`
    PRAGMA journal_mode = WAL;
    PRAGMA busy_timeout = 5000;
    CREATE TABLE IF NOT EXISTS generations (
      id TEXT PRIMARY KEY, prompt TEXT NOT NULL, model TEXT NOT NULL,
      size TEXT NOT NULL, quality TEXT NOT NULL, background TEXT NOT NULL,
      n INTEGER NOT NULL, basic_mode INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL, error TEXT,
      created_at TEXT NOT NULL, completed_at TEXT, images TEXT NOT NULL DEFAULT '[]'
    );
    CREATE INDEX IF NOT EXISTS generations_created_at ON generations(created_at DESC);
  `);
  db.prepare("UPDATE generations SET status = 'failed', error = ?, completed_at = ? WHERE status IN ('queued', 'generating')")
    .run('服务已重启，未完成的任务已停止。请复用提示词重新生成。', new Date().toISOString());

  const app = express();
  app.disable('x-powered-by');
  app.get('/healthz', (_req, res) => res.json({ status: 'ok' }));
  app.use((req, res, next) => {
    res.set('X-Content-Type-Options', 'nosniff');
    res.set('Referrer-Policy', 'same-origin');
    res.set('X-Frame-Options', 'DENY');
    res.set('Content-Security-Policy', "default-src 'self'; img-src 'self' blob: data:; style-src 'self'; script-src 'self'; font-src 'self'; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'");
    if (authPassword) {
      const header = req.get('authorization') || '';
      let credentials = '';
      if (header.startsWith('Basic ')) credentials = Buffer.from(header.slice(6), 'base64').toString('utf8');
      if (!constantEquals(credentials, `${authUsername}:${authPassword}`)) {
        res.set('WWW-Authenticate', 'Basic realm="sub2api Image Studio", charset="UTF-8"');
        return res.status(401).send('请输入访问用户名和密码。');
      }
    }
    if (!['GET', 'HEAD', 'OPTIONS'].includes(req.method)) {
      const origin = req.get('origin');
      const host = req.get('host');
      let sameOrigin = !origin;
      try { sameOrigin = !origin || new URL(origin).host === host; } catch { sameOrigin = false; }
      if (!sameOrigin || req.get('sec-fetch-site') === 'cross-site') return res.status(403).json({ error: '请从本应用页面提交请求。' });
      const contentType = (req.get('content-type') || '').split(';', 1)[0].trim().toLowerCase();
      if (contentType !== 'application/json') return res.status(415).json({ error: '请求必须使用 application/json。' });
    }
    next();
  });
  app.use('/api', (_req, res, next) => { res.set('Cache-Control', 'no-store'); next(); });
  app.use(express.json({ limit: '100kb' }));

  function publicConfig() {
    return { baseUrl: config.baseUrl, model: config.model, hasApiKey: Boolean(config.apiKey), configured: Boolean(config.baseUrl && config.apiKey), timeoutSeconds };
  }
  function generationFromRow(row) {
    if (!row) throw httpError(404, '未找到这条生成记录。');
    return {
      id: row.id, prompt: row.prompt, model: row.model, size: row.size,
      quality: row.quality, background: row.background, n: row.n, basicMode: Boolean(row.basic_mode),
      status: row.status, error: row.error,
      createdAt: row.created_at, completedAt: row.completed_at, images: JSON.parse(row.images),
    };
  }
  const getGeneration = id => generationFromRow(db.prepare('SELECT * FROM generations WHERE id = ?').get(id));
  function requireConfig() {
    if (!config.baseUrl || !config.apiKey) throw httpError(400, '请先在连接设置中填写 sub2api 地址和 API Key。');
  }

  app.get('/api/config', (_req, res) => res.json(publicConfig()));
  app.put('/api/config', (req, res) => {
    const body = req.body;
    if (!body || typeof body !== 'object' || Array.isArray(body)) throw httpError(400, '连接设置格式无效。');
    if (body.apiKey !== undefined && (typeof body.apiKey !== 'string' || body.apiKey.length > 4096 || /[\r\n]/.test(body.apiKey))) {
      throw httpError(400, 'API Key 格式无效。');
    }
    const updated = {
      baseUrl: normalizeBaseUrl(body.baseUrl ?? config.baseUrl),
      apiKey: body.clearApiKey === true ? '' : body.apiKey?.trim() || config.apiKey,
      model: validateModel(body.model ?? config.model),
    };
    writeFileSync(`${configFile}.tmp`, JSON.stringify(updated, null, 2), { mode: 0o600 });
    renameSync(`${configFile}.tmp`, configFile);
    config = updated;
    res.json(publicConfig());
  });
  app.get('/api/models', async (_req, res) => {
    requireConfig();
    const snapshot = { ...config };
    try {
      const response = await fetch(`${snapshot.baseUrl}/v1/models`, {
        headers: { Authorization: `Bearer ${snapshot.apiKey}` }, signal: AbortSignal.timeout(15000), redirect: 'error',
      });
      const body = await readUpstreamJson(response);
      if (!Array.isArray(body.data)) throw new Error('模型列表格式无效，请手动填写模型名称。');
      const models = body.data.filter(item => typeof item?.id === 'string' && /image|imagine|dall-e/i.test(item.id)).map(item => ({ id: item.id }));
      res.json({ models });
    } catch (error) { throw httpError(502, safeMessage(error.message, snapshot.apiKey)); }
  });

  const queue = [];
  let worker = null;
  let closed = false;
  let activeController = null;

  async function generate(id, snapshot) {
    const generation = getGeneration(id);
    const controller = new AbortController();
    activeController = controller;
    const timer = setTimeout(() => controller.abort(new Error('生图超时，请检查上游任务状态后再重试，避免重复计费。')), timeoutSeconds * 1000);
    const files = [];
    const images = [];
    try {
      db.prepare("UPDATE generations SET status = 'generating' WHERE id = ?").run(id);
      const payload = generation.basicMode
        ? { prompt: generation.prompt, response_format: 'b64_json' }
        : { model: generation.model, prompt: generation.prompt, n: generation.n, size: generation.size };
      // Leave automatic options out so both GPT Image and DALL-E gateways can use their defaults.
      if (!generation.basicMode) {
        if (generation.quality !== 'auto') payload.quality = generation.quality;
        if (generation.background !== 'auto') payload.background = generation.background;
        if (/^dall-e-/i.test(generation.model)) payload.response_format = 'b64_json';
      }
      const response = await fetch(`${snapshot.baseUrl}/v1/images/generations`, {
        method: 'POST', headers: { Authorization: `Bearer ${snapshot.apiKey}`, 'Content-Type': 'application/json' },
        body: JSON.stringify(payload), signal: controller.signal, redirect: 'error',
      });
      const body = await readUpstreamJson(response);
      if (!Array.isArray(body.data) || !body.data.length || body.data.length > 4) throw new Error('上游未返回有效图片列表（data），请确认模型支持图片生成接口。');
      for (const item of body.data) {
        if (!item || typeof item !== 'object') throw new Error('上游图片数据格式无效。');
        let bytes;
        if (typeof item.b64_json === 'string' && item.b64_json) {
          if (item.b64_json.length > Math.ceil(maxImageBytes / 3) * 4 + 4 || !/^[A-Za-z0-9+/\r\n]+={0,2}$/.test(item.b64_json)) {
            throw new Error('图片 Base64 数据无效或超过 30 MB。');
          }
          bytes = Buffer.from(item.b64_json, 'base64');
        } else if (typeof item.url === 'string') {
          const imageUrl = new URL(item.url);
          if (!['http:', 'https:'].includes(imageUrl.protocol) || imageUrl.username || imageUrl.password) throw new Error('上游图片下载地址无效。');
          // Returned URLs may point to a different storage provider. Never forward the API key.
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
        // Preserve each successful image even if another download fails or the service restarts.
        db.prepare('UPDATE generations SET images = ? WHERE id = ?').run(JSON.stringify(images), id);
      }
      db.prepare("UPDATE generations SET status = 'completed', images = ?, completed_at = ? WHERE id = ?")
        .run(JSON.stringify(images), new Date().toISOString(), id);
    } catch (error) {
      const retainedFiles = new Set(images.map(image => path.join(imageDir, path.basename(image.url))));
      await Promise.allSettled(files.filter(file => !retainedFiles.has(file)).map(file => unlink(file)));
      let message = controller.signal.aborted
        ? (closed ? '服务正在停止，任务已中断。请检查上游状态后再重试。' : '生图超时，请检查上游任务状态后再重试，避免重复计费。')
        : safeMessage(error.message, snapshot.apiKey);
      if (/capability|no available accounts|no available account|无可用账号|无可用账户/i.test(message)) {
        message += ' 请检查分组与模型权限；basic 分组请启用基础兼容模式。';
      }
      if (images.length) message += ` 已保留 ${images.length} 张成功下载的图片，可在详情中预览和下载。`;
      db.prepare("UPDATE generations SET status = 'failed', error = ?, completed_at = ? WHERE id = ?")
        .run(message, new Date().toISOString(), id);
    } finally {
      clearTimeout(timer);
      activeController = null;
    }
  }

  function startWorker() {
    if (worker || closed) return;
    worker = Promise.resolve().then(async () => {
      while (queue.length && !closed) {
        const item = queue.shift();
        await generate(item.id, item.config);
      }
    }).finally(() => { worker = null; });
  }

  app.post('/api/generations', (req, res) => {
    requireConfig();
    if (closed) throw httpError(503, '服务正在停止，请稍后再试。');
    if (queue.length + Number(Boolean(activeController)) >= 20) throw httpError(429, '当前待处理任务较多，请稍后再提交。');
    const body = req.body;
    if (!body || typeof body !== 'object' || Array.isArray(body)) throw httpError(400, '生成参数格式无效。');
    if (typeof body.prompt !== 'string' || !body.prompt.trim() || body.prompt.length > 16000) throw httpError(400, '请输入提示词，最多 16000 字符。');
    const model = validateModel(body.model ?? config.model);
    const size = body.size ?? '1024x1024';
    const quality = body.quality ?? 'auto';
    const background = body.background ?? 'auto';
    const n = body.n ?? 1;
    const basicMode = body.basicMode ?? false;
    if (typeof basicMode !== 'boolean' || (basicMode && n !== 1)) throw httpError(400, '基础兼容模式每次只能生成一张图片。');
    if (!sizes.has(size) || !qualities.has(quality) || !backgrounds.has(background) || !Number.isInteger(n) || n < 1 || n > 4) {
      throw httpError(400, '图片尺寸、质量、背景或数量不受支持。图片数量必须为 1 至 4。');
    }
    const id = randomUUID();
    db.prepare('INSERT INTO generations (id,prompt,model,size,quality,background,n,basic_mode,status,created_at) VALUES (?,?,?,?,?,?,?,?,?,?)')
      .run(id, body.prompt.trim(), model, size, quality, background, n, Number(basicMode), 'queued', new Date().toISOString());
    const generation = getGeneration(id);
    queue.push({ id, config: { ...config } });
    startWorker();
    res.status(202).json({ generation });
  });

  app.get('/api/generations', (req, res) => {
    const query = typeof req.query.q === 'string' ? req.query.q.trim().slice(0, 500) : '';
    const status = req.query.status ?? 'all';
    if (!['all', 'completed', 'failed', 'processing'].includes(status)) throw httpError(400, '历史记录状态筛选无效。');
    const page = Number(req.query.page ?? 1);
    const limit = Number(req.query.limit ?? 24);
    if (!Number.isInteger(page) || page < 1 || !Number.isInteger(limit) || limit < 1 || limit > 100) throw httpError(400, '分页参数无效。');
    let condition = 'WHERE 1=1';
    const params = [];
    if (query) { condition += " AND (prompt LIKE ? ESCAPE '\\' OR model LIKE ? ESCAPE '\\')"; const match = `%${query.replace(/[\\%_]/g, '\\$&')}%`; params.push(match, match); }
    if (status === 'processing') condition += " AND status IN ('queued','generating')";
    else if (status !== 'all') { condition += ' AND status = ?'; params.push(status); }
    const total = db.prepare(`SELECT count(*) AS n FROM generations ${condition}`).get(...params).n;
    const items = db.prepare(`SELECT * FROM generations ${condition} ORDER BY created_at DESC, rowid DESC LIMIT ? OFFSET ?`).all(...params, limit, (page - 1) * limit).map(generationFromRow);
    const counts = db.prepare('SELECT status, count(*) AS n FROM generations GROUP BY status').all();
    const stats = { total: 0, completed: 0, processing: 0, failed: 0, images: 0 };
    for (const item of counts) { stats.total += item.n; stats[['queued', 'generating'].includes(item.status) ? 'processing' : item.status] += item.n; }
    stats.images = db.prepare('SELECT coalesce(sum(json_array_length(images)),0) AS n FROM generations').get().n;
    res.json({ items, total, page, pages: Math.ceil(total / limit), stats });
  });
  app.get('/api/generations/:id', (req, res) => res.json({ generation: getGeneration(req.params.id) }));
  app.delete('/api/generations/:id', async (req, res) => {
    const generation = getGeneration(req.params.id);
    if (['queued', 'generating'].includes(generation.status)) throw httpError(409, '任务正在处理中，完成后才能删除。');
    // Delete the record only after all known files have been removed; missing files are harmless.
    for (const image of generation.images) {
      const filename = path.basename(image.url);
      try { await unlink(path.join(imageDir, filename)); } catch (error) { if (error.code !== 'ENOENT') throw error; }
    }
    db.prepare('DELETE FROM generations WHERE id = ?').run(generation.id);
    res.status(204).end();
  });
  app.get('/api/images/:filename/download', (req, res) => {
    if (!/^[\da-f-]{36}\.(png|jpg|webp)$/.test(req.params.filename)) throw httpError(404, '图片不存在。');
    res.download(path.join(imageDir, req.params.filename), `sub2api-${req.params.filename}`, error => {
      if (error && !res.headersSent) res.status(error.statusCode || 500).json({ error: '图片不存在或暂时无法下载。' });
    });
  });
  app.use('/images', express.static(imageDir, {
    index: false, dotfiles: 'deny', maxAge: '1d', fallthrough: false,
    setHeaders(res) { res.setHeader('Cache-Control', 'private, max-age=86400'); },
  }));
  app.use('/vendor/phosphor', express.static(path.join(rootDir, 'node_modules/@phosphor-icons/web/src'), { index: false, dotfiles: 'deny', maxAge: '7d' }));
  app.use(express.static(path.join(rootDir, 'public'), { index: 'index.html', dotfiles: 'deny' }));
  app.use((_req, res) => res.status(404).json({ error: '页面或接口不存在。' }));
  app.use((error, _req, res, _next) => {
    const status = error.type === 'entity.parse.failed' ? 400 : error.status || 500;
    const message = error.type === 'entity.parse.failed' ? '请求 JSON 格式无效。'
      : status >= 500 && status !== 502 ? '服务器处理失败，请检查数据目录权限和磁盘空间。' : safeMessage(error.message, config.apiKey);
    if (!res.headersSent) res.status(status).json({ error: message });
  });

  return {
    app,
    async close() {
      closed = true;
      activeController?.abort();
      if (worker) await worker;
      for (const item of queue) db.prepare("UPDATE generations SET status='failed', error=?, completed_at=? WHERE id=?")
        .run('服务正在停止，任务尚未开始。请重新提交。', new Date().toISOString(), item.id);
      queue.length = 0;
      db.close();
    },
  };
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const envFile = path.join(rootDir, '.env');
  if (existsSync(envFile)) process.loadEnvFile(envFile);
  const application = createApp();
  const port = Number(process.env.PORT) || 3000;
  const host = process.env.HOST || '0.0.0.0';
  const server = application.app.listen(port, host, () => console.log(`sub2api 图片工作台已启动：http://${host}:${port}`));
  let stopping = false;
  async function stop() {
    if (stopping) return;
    stopping = true;
    const deadline = setTimeout(() => process.exit(1), 10000).unref();
    server.close();
    await application.close();
    clearTimeout(deadline);
  }
  process.on('SIGTERM', stop);
  process.on('SIGINT', stop);
}
