import { createServer } from 'node:http';
import { createHash, timingSafeEqual } from 'node:crypto';
import { ApiError, openid, text } from './config.js';
import { HELP, invokeCommand, parseCommand } from './commands.js';
import { Store } from './store.js';

const digest = value => createHash('sha256').update(value).digest();

export class ChannelService {
  constructor(config, bot, { clock = Date.now, fetcher = fetch } = {}) {
    this.config = config;
    this.bot = bot;
    this.clock = clock;
    this.fetcher = fetcher;
    this.store = new Store(config, clock);
    this.connected = false;
    this.busy = false;
    this.server = createServer((req, res) => this.handleHttp(req, res));
    this.server.requestTimeout = 15000;
    this.server.headersTimeout = 10000;
    this.server.maxHeadersCount = 40;
    this.store.prune();
  }

  authorized(event) {
    return Boolean(this.config.commandUrl) && this.config.users.includes(event.sender)
      && (event.scope === 'c2c' || this.config.groups.includes(event.targetId));
  }

  ingest(message) {
    const target = message?.replyTarget;
    if (!target || !['c2c', 'group'].includes(target.scope) || message.kind !== target.scope
      || typeof message.senderId !== 'string' || !openid.test(message.senderId)
      || typeof target.targetId !== 'string' || !openid.test(target.targetId) || message.senderIsBot
      || typeof message.messageId !== 'string' || !message.messageId || message.messageId.length > 512
      || typeof message.content !== 'string' || Buffer.byteLength(message.content) > 8192
      || (target.scope === 'c2c' && (message.rawEventType !== 'C2C_MESSAGE_CREATE' || target.targetId !== message.senderId))
      || (target.scope === 'group' && (message.rawEventType !== 'GROUP_AT_MESSAGE_CREATE' || target.targetId !== message.groupOpenid))) return false;
    const value = message.content.trim().replace(/^<@!?[A-Za-z0-9_-]+>\s*/, '').trim();
    const parsed = parseCommand(value);
    if (!parsed) return false;
    const event = { scope: target.scope, targetId: target.targetId, sender: message.senderId, messageId: message.messageId, text: value };
    if (!parsed.local && !this.authorized(event)) return false;
    return this.store.admit(event);
  }

  async handleHttp(req, res) {
    const send = (status, value) => {
      if (!res.destroyed) res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', 'Cache-Control': 'no-store', 'X-Content-Type-Options': 'nosniff' }).end(JSON.stringify(value));
    };
    try {
      const path = new URL(req.url, 'http://localhost').pathname;
      if (req.method === 'GET' && ['/healthz', '/readyz'].includes(path)) {
        this.store.db.prepare('SELECT 1').get();
        send(path === '/readyz' && !this.connected ? 503 : 200, { ok: true, connected: this.connected, commands: Boolean(this.config.commandUrl) });
        return;
      }
      if (!path.startsWith('/api/')) throw new ApiError('not_found', 404);
      if (!timingSafeEqual(digest(req.headers.authorization ?? ''), digest('Bearer ' + this.config.apiToken))) throw new ApiError('unauthorized', 401);
      if (path === '/api/targets' && req.method === 'GET') { send(200, { ok: true, targets: this.store.targets() }); return; }
      if (path === '/api/bindings' && req.method === 'POST') {
        const body = await readBody(req);
        send(201, { ok: true, ...this.store.binding(body.name, body.type) }); return;
      }
      if (path === '/api/notifications' && req.method === 'POST') {
        const body = await readBody(req);
        const id = this.store.enqueue(body.message, body.targets, body.idempotency_key);
        send(202, { ok: true, status: 'queued', id }); return;
      }
      const notification = /^\/api\/notifications\/([a-f0-9]{40})(\/retry)?$/.exec(path);
      if (notification && req.method === (notification[2] ? 'POST' : 'GET')) {
        if (notification[2]) send(202, { ok: true, status: 'queued', id: notification[1], retried_targets: this.store.retry(notification[1]) });
        else send(200, this.store.notification(notification[1]));
        return;
      }
      throw new ApiError('not_found', 404);
    } catch (error) {
      send(error instanceof ApiError ? error.status : 503, { ok: false, error: error instanceof ApiError ? error.message : 'service_unavailable' });
    }
  }

  async tick() {
    if (this.busy || !this.connected) return;
    this.busy = true;
    try { await this.deliver(); await this.processInbox(); }
    finally { this.busy = false; }
  }

  async deliver() {
    const db = this.store.db;
    const row = db.prepare(`SELECT d.*,n.message FROM deliveries d JOIN notifications n ON n.id=d.notification_id
      WHERE d.status=0 AND d.next_at<=? ORDER BY n.created,d.target LIMIT 1`).get(this.clock());
    if (!row) return;
    const attempt = row.attempts + 1;
    db.prepare('UPDATE deliveries SET attempts=?,next_at=? WHERE notification_id=? AND target=?').run(attempt, this.clock() + backoff(attempt), row.notification_id, row.target);
    try {
      const receipt = await this.bot.sendText({ scope: row.kind === 'user' ? 'c2c' : 'group', targetId: row.openid }, row.message);
      if (typeof receipt?.id !== 'string' || !receipt.id || receipt.id.length > 512) throw new Error('missing_receipt');
      db.prepare('UPDATE deliveries SET status=1,last_error=NULL,receipt=? WHERE notification_id=? AND target=?').run(receipt.id, row.notification_id, row.target);
    } catch (error) {
      const code = Number.isInteger(error?.httpStatus) ? `qq_http_${error.httpStatus}` : 'qq_delivery_failed';
      db.prepare('UPDATE deliveries SET status=?,last_error=? WHERE notification_id=? AND target=?').run(attempt >= 5 ? 2 : 0, code, row.notification_id, row.target);
    }
  }

  async processInbox() {
    const db = this.store.db;
    const row = db.prepare('SELECT * FROM inbox WHERE status IN (0,1) AND next_at<=? ORDER BY created,rowid LIMIT 1').get(this.clock());
    if (!row) return;
    const event = JSON.parse(row.event);
    const parsed = parseCommand(event.text);
    if (!parsed || (!parsed.local && !this.authorized(event))) {
      db.prepare('UPDATE inbox SET status=3 WHERE id=?').run(row.id); return;
    }
    let response = row.response;
    if (row.status === 0) {
      const attempt = row.attempts + 1;
      db.prepare('UPDATE inbox SET attempts=?,next_at=? WHERE id=?').run(attempt, this.clock() + backoff(attempt), row.id);
      try {
        if (parsed.local === 'help') response = HELP;
        else if (parsed.local === 'identity') response = `用户 OpenID：${event.sender}` + (event.scope === 'group' ? `\n群 OpenID：${event.targetId}` : '');
        else if (parsed.local === 'bind') response = this.store.bind(parsed.code, event);
        else response = await invokeCommand(this.config, event, parsed, this.fetcher, this.clock());
      } catch {
        if (attempt < 5) return;
        response = '无法确认博客操作结果，请到后台检查后再操作。';
      }
      db.prepare('UPDATE inbox SET status=1,response=?,attempts=0,next_at=0 WHERE id=?').run(text(response), row.id);
    }
    const attempts = row.status === 1 ? row.attempts + 1 : 1;
    // Persist the result before replying: a QQ failure must not repeat a blog operation.
    db.prepare('UPDATE inbox SET attempts=?,next_at=? WHERE id=?').run(attempts, this.clock() + backoff(attempts), row.id);
    try {
      const receipt = await this.bot.sendText({ scope: event.scope, targetId: event.targetId, msgId: event.messageId }, text(response));
      if (typeof receipt?.id !== 'string' || !receipt.id) throw new Error('missing_receipt');
      db.prepare('UPDATE inbox SET status=2 WHERE id=?').run(row.id);
    } catch {
      if (attempts >= 5) db.prepare('UPDATE inbox SET status=3 WHERE id=?').run(row.id);
    }
  }

  listen() {
    return new Promise((resolve, reject) => { this.server.once('error', reject); this.server.listen(this.config.port, this.config.host, resolve); });
  }

  startWorker() {
    this.timer = setInterval(() => this.tick().catch(() => console.error('QQ channel worker storage failure')), 250);
    this.pruneTimer = setInterval(() => { try { this.store.prune(); } catch { console.error('QQ channel retention cleanup failed'); } }, 3600000);
  }

  async close() {
    clearInterval(this.timer); clearInterval(this.pruneTimer);
    await new Promise(resolve => this.server.close(resolve));
    while (this.busy) await new Promise(resolve => setTimeout(resolve, 20));
    this.store.close();
  }
}

function backoff(attempt) { return Math.min(300000, 1000 * 2 ** attempt); }

async function readBody(req) {
  if ((req.headers['content-type'] ?? '').split(';')[0].trim().toLowerCase() !== 'application/json') throw new ApiError('json_required', 415);
  const chunks = [];
  let length = 0;
  await new Promise((resolve, reject) => {
    const cleanup = () => { req.off('data', data); req.off('end', end); req.off('error', error); req.off('aborted', aborted); };
    const error = cause => { cleanup(); reject(cause); };
    const aborted = () => error(new ApiError('request_aborted'));
    const end = () => { cleanup(); resolve(); };
    const data = chunk => {
      length += chunk.length;
      if (length > 65536) { error(new ApiError('body_too_large', 413)); req.resume(); }
      else chunks.push(chunk);
    };
    req.on('data', data); req.once('end', end); req.once('error', error); req.once('aborted', aborted);
  });
  let body;
  try { body = JSON.parse(Buffer.concat(chunks).toString('utf8')); } catch { throw new ApiError('invalid_json'); }
  if (!body || typeof body !== 'object' || Array.isArray(body)) throw new ApiError('invalid_json');
  return body;
}
