import { DatabaseSync } from 'node:sqlite';
import { createHash, randomBytes } from 'node:crypto';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { alias, ApiError } from './config.js';

export const hash = value => createHash('sha256').update(value).digest('hex');

export class Store {
  constructor(config, clock = Date.now) {
    this.clock = clock;
    mkdirSync(config.dataDir, { recursive: true, mode: 0o700 });
    this.db = new DatabaseSync(join(config.dataDir, 'channel.sqlite'));
    this.db.exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
      CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
      CREATE TABLE IF NOT EXISTS targets (name TEXT PRIMARY KEY, kind TEXT NOT NULL, openid TEXT NOT NULL, bound_by TEXT NOT NULL, updated INTEGER NOT NULL);
      CREATE TABLE IF NOT EXISTS bindings (hash TEXT PRIMARY KEY, name TEXT NOT NULL, kind TEXT NOT NULL, expires INTEGER NOT NULL);
      CREATE TABLE IF NOT EXISTS notifications (id TEXT PRIMARY KEY, key TEXT UNIQUE NOT NULL, fingerprint TEXT NOT NULL, message TEXT NOT NULL, created INTEGER NOT NULL);
      CREATE TABLE IF NOT EXISTS deliveries (notification_id TEXT NOT NULL, target TEXT NOT NULL, kind TEXT NOT NULL, openid TEXT NOT NULL,
        status INTEGER NOT NULL DEFAULT 0, attempts INTEGER NOT NULL DEFAULT 0, next_at INTEGER NOT NULL DEFAULT 0, last_error TEXT, receipt TEXT,
        PRIMARY KEY(notification_id,target));
      CREATE TABLE IF NOT EXISTS inbox (id TEXT PRIMARY KEY, sender TEXT NOT NULL, event TEXT NOT NULL, response TEXT,
        status INTEGER NOT NULL DEFAULT 0, attempts INTEGER NOT NULL DEFAULT 0, next_at INTEGER NOT NULL DEFAULT 0, created INTEGER NOT NULL);
      CREATE INDEX IF NOT EXISTS deliveries_pending ON deliveries(status,next_at);
      CREATE INDEX IF NOT EXISTS inbox_pending ON inbox(status,next_at);
      CREATE INDEX IF NOT EXISTS inbox_sender ON inbox(sender,created);`);
    const previous = this.db.prepare('SELECT value FROM meta WHERE key=?').get('app_id');
    if (previous && previous.value !== config.appId) { this.db.close(); throw new Error('Data volume belongs to a different QQ AppID'); }
    this.db.prepare('INSERT OR IGNORE INTO meta(key,value) VALUES(?,?)').run('app_id', config.appId);
  }

  transaction(callback) {
    this.db.exec('BEGIN IMMEDIATE');
    try { const result = callback(); this.db.exec('COMMIT'); return result; }
    catch (error) { this.db.exec('ROLLBACK'); throw error; }
  }

  binding(name, kind) {
    if (typeof name !== 'string' || !alias.test(name) || !['user', 'group'].includes(kind)) throw new ApiError('invalid_binding');
    const code = randomBytes(16).toString('hex');
    this.db.prepare('DELETE FROM bindings WHERE name=? OR expires<?').run(name, this.clock());
    this.db.prepare('INSERT INTO bindings VALUES(?,?,?,?)').run(hash(code), name, kind, this.clock() + 600000);
    return { name, type: kind, command: '绑定 ' + code, expires_in: 600 };
  }

  bind(code, event) {
    return this.transaction(() => {
      const record = this.db.prepare('SELECT * FROM bindings WHERE hash=? AND expires>=?').get(hash(code), this.clock());
      if (!record || record.kind !== (event.scope === 'c2c' ? 'user' : 'group')) return '绑定口令无效、类型不匹配或已过期。';
      this.db.prepare('INSERT OR REPLACE INTO targets VALUES(?,?,?,?,?)').run(record.name, record.kind, event.targetId, event.sender, this.clock());
      this.db.prepare('DELETE FROM bindings WHERE hash=?').run(hash(code));
      return `绑定成功：${record.name}\n用户 OpenID：${event.sender}` + (event.scope === 'group' ? `\n群 OpenID：${event.targetId}` : '');
    });
  }

  targets() { return this.db.prepare('SELECT name,kind AS type,openid,bound_by,updated FROM targets ORDER BY name').all(); }

  enqueue(message, names, key) {
    if (typeof message !== 'string' || !message.trim() || Buffer.byteLength(message) > 1800
      || /[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/.test(message)
      || !Array.isArray(names) || names.length < 1 || names.length > 20 || names.some(name => typeof name !== 'string' || !alias.test(name))
      || typeof key !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(key)) throw new ApiError('invalid_notification');
    const unique = [...new Set(names)].sort();
    const fingerprint = hash(JSON.stringify([message, unique]));
    return this.transaction(() => {
      const existing = this.db.prepare('SELECT * FROM notifications WHERE key=?').get(key);
      if (existing) {
        if (existing.fingerprint !== fingerprint) throw new ApiError('idempotency_conflict', 409);
        return existing.id;
      }
      if (this.db.prepare('SELECT COUNT(*) AS n FROM deliveries WHERE status=0').get().n + unique.length > 10000) throw new ApiError('queue_full', 503);
      const targets = unique.map(name => {
        const target = this.db.prepare('SELECT * FROM targets WHERE name=?').get(name);
        if (!target) throw new ApiError('target_not_bound');
        return target;
      });
      const id = randomBytes(20).toString('hex');
      this.db.prepare('INSERT INTO notifications VALUES(?,?,?,?,?)').run(id, key, fingerprint, message, this.clock());
      for (const target of targets) this.db.prepare('INSERT INTO deliveries(notification_id,target,kind,openid) VALUES(?,?,?,?)').run(id, target.name, target.kind, target.openid);
      return id;
    });
  }

  notification(id) {
    const item = this.db.prepare('SELECT id,created FROM notifications WHERE id=?').get(id);
    if (!item) throw new ApiError('notification_not_found', 404);
    const targets = this.db.prepare('SELECT target,status,attempts,last_error,receipt FROM deliveries WHERE notification_id=? ORDER BY target').all(id);
    return { ok: true, ...item, status: targets.some(row => row.status === 0) ? 'queued' : targets.some(row => row.status === 2) ? 'failed' : 'sent', targets };
  }

  retry(id) {
    this.notification(id);
    return this.db.prepare('UPDATE deliveries SET status=0,attempts=0,next_at=0,last_error=NULL WHERE notification_id=? AND status=2').run(id).changes;
  }

  admit(event) {
    const id = hash(JSON.stringify([event.scope, event.targetId, event.messageId]));
    if (this.db.prepare('SELECT 1 FROM inbox WHERE id=?').get(id)) return false;
    const count = this.db.prepare('SELECT COUNT(*) AS n FROM inbox WHERE sender=? AND created>?').get(event.sender, this.clock() - 60000).n;
    if (count >= 10 || this.db.prepare('SELECT COUNT(*) AS n FROM inbox WHERE status IN (0,1)').get().n >= 10000) return false;
    this.db.prepare('INSERT OR IGNORE INTO inbox(id,sender,event,created) VALUES(?,?,?,?)').run(id, event.sender, JSON.stringify(event), this.clock());
    return true;
  }

  prune() {
    const cutoff = this.clock() - 30 * 86400000;
    this.transaction(() => {
      this.db.prepare('DELETE FROM inbox WHERE status IN (2,3) AND created<?').run(cutoff);
      this.db.prepare('DELETE FROM deliveries WHERE notification_id IN (SELECT id FROM notifications WHERE created<?) AND notification_id NOT IN (SELECT notification_id FROM deliveries WHERE status=0)').run(cutoff);
      this.db.prepare('DELETE FROM notifications WHERE created<? AND id NOT IN (SELECT notification_id FROM deliveries)').run(cutoff);
      this.db.prepare('DELETE FROM bindings WHERE expires<?').run(this.clock());
    });
  }

  close() { this.db.close(); }
}
