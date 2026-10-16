import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createServer } from 'node:http';
import { createHmac } from 'node:crypto';
import { WebSocketServer } from 'ws';
import { QQBot } from '@tencent-connect/qqbot-nodejs';
import { settings, text } from '../src/config.js';
import { commandBody, invokeCommand, parseCommand } from '../src/commands.js';
import { ChannelService } from '../src/service.js';

const environment = { QQ_APP_ID: '12345', QQ_APP_SECRET: 'test-only-secret', BOT_API_TOKEN: 'a'.repeat(48) };
const admin = { ENHANCEMENT_COMMAND_URL: 'http://127.0.0.1/action/enhancement-edit?do=qq-agent-command',
  ENHANCEMENT_COMMAND_SECRET: 'b'.repeat(48), QQ_ALLOWED_USERS: 'owner', QQ_ALLOWED_GROUPS: 'admins' };
function fixture(t, options = {}) {
  const dataDir = mkdtempSync(join(tmpdir(), 'qq-channel-'));
  let now = Date.now();
  const calls = [];
  const config = settings({ ...environment, ...admin, BOT_DATA_DIR: dataDir, BOT_PORT: '0', ...options.env });
  const bot = { async sendText(target, message) { calls.push({ target, message }); return { id: 'receipt-' + calls.length }; } };
  const service = new ChannelService(config, bot, { clock: () => now, ...options.dependencies });
  service.connected = true;
  t.after(async () => { await service.close(); rmSync(dataDir, { recursive: true, force: true }); });
  return { config, bot, service, calls, advance: () => { now += 600001; }, now: () => now };
}
function incoming(content, id = 'message-1', scope = 'c2c', sender = 'owner', targetId = scope === 'c2c' ? sender : 'admins') {
  return { kind: scope, rawEventType: scope === 'c2c' ? 'C2C_MESSAGE_CREATE' : 'GROUP_AT_MESSAGE_CREATE',
    senderId: sender, content, messageId: id, groupOpenid: scope === 'group' ? targetId : undefined,
    replyTarget: { scope, targetId, msgId: id } };
}
function bind(service, name = 'owner', kind = 'user', targetId = name) {
  const binding = service.store.binding(name, kind);
  assert.match(service.store.bind(binding.command.split(' ')[1], { scope: kind === 'user' ? 'c2c' : 'group', targetId, sender: 'owner' }), /绑定成功/);
  return binding;
}
async function api(service, path, body, token = environment.BOT_API_TOKEN) {
  if (!service.server.listening) await service.listen();
  const response = await fetch(`http://127.0.0.1:${service.server.address().port}${path}`, {
    method: body === undefined ? 'GET' : 'POST', headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' },
    ...(body === undefined ? {} : { body: typeof body === 'string' ? body : JSON.stringify(body) }),
  });
  return { status: response.status, body: await response.json() };
}

test('configuration validates credentials, command URL, separate secrets and explicit allowlists', () => {
  assert.equal(settings(environment).commandUrl, '');
  assert.equal(settings({ ...environment, ...admin }).users[0], 'owner');
  for (const values of [{ QQ_APP_ID: 'NaN' }, { BOT_API_TOKEN: 'short' }, { BOT_PORT: '65536' },
    { ...admin, ENHANCEMENT_COMMAND_URL: 'ftp://example.test' }, { ...admin, ENHANCEMENT_COMMAND_URL: 'https://user:pass@example.test' },
    { ...admin, QQ_ALLOWED_USERS: '*' }, { ...admin, QQ_ALLOWED_USERS: '' }, { ...admin, ENHANCEMENT_COMMAND_SECRET: environment.BOT_API_TOKEN }]) {
    assert.throws(() => settings({ ...environment, ...values }));
  }
});

test('fixed command grammar preserves literal replies and rejects extra operations', () => {
  assert.equal(parseCommand('/帮助').local, 'help');
  assert.equal(parseCommand('我的ID').local, 'identity');
  assert.deepEqual(parseCommand('待审评论 20'), { command: 'comments.list', args: { status: 'waiting', limit: 20 } });
  assert.deepEqual(parseCommand('待审友链'), { command: 'links.list', args: { state: '0', limit: 5 } });
  assert.equal(parseCommand('回复评论 42：中文 `literal` $(data)').args.text, '中文 `literal` $(data)');
  for (const value of ['通过评论 1; rm', '通过评论 0', '通过评论 2147483648', '通过评论 1\n通过评论 2', '待审评论 21', '回复评论 1:', '删除评论 1']) assert.equal(parseCommand(value), null, value);
  assert.equal(parseCommand('回复评论 1：' + '中'.repeat(1334)), null);
  const bounded = text('<b>中文🙂</b>'.repeat(500));
  assert.ok(Buffer.byteLength(bounded) <= 1800);
  assert.ok(!bounded.includes('�') && !bounded.includes('<b>'));
});

test('HTTP API authenticates, validates JSON and enforces body limit with usable 413 response', async t => {
  const { service } = fixture(t);
  assert.equal((await api(service, '/healthz')).status, 200);
  service.connected = false;
  assert.equal((await api(service, '/readyz')).status, 503);
  assert.equal((await api(service, '/api/targets', undefined, 'bad')).status, 401);
  for (const body of ['{', 'null', '[]', '{}', '{"name":42,"type":"user"}']) assert.equal((await api(service, '/api/bindings', body)).status, 400);
  assert.equal((await api(service, '/api/bindings', ' '.repeat(65537))).status, 413);
  assert.equal((await api(service, '/api/unknown')).status, 404);
  const binding = await api(service, '/api/bindings', { name: 'owner', type: 'user' });
  assert.equal(binding.status, 201);
  assert.ok(!JSON.stringify(binding).includes(environment.QQ_APP_SECRET));
});

test('binding codes are single use, expire, require matching type and persist targets', t => {
  const { service, advance } = fixture(t);
  const value = service.store.binding('owner', 'user').command.split(' ')[1];
  const event = { scope: 'group', targetId: 'admins', sender: 'owner' };
  assert.match(service.store.bind(value, event), /类型不匹配/);
  event.scope = 'c2c'; event.targetId = 'owner';
  assert.match(service.store.bind(value, event), /绑定成功/);
  assert.match(service.store.bind(value, event), /无效/);
  const expired = service.store.binding('expired', 'group').command.split(' ')[1];
  advance();
  assert.match(service.store.bind(expired, { ...event, scope: 'group' }), /过期/);
  assert.equal(service.store.targets().length, 1);
});

test('notifications use target snapshots, deduplicate exact requests and reject key conflicts', async t => {
  const { service, calls } = fixture(t);
  bind(service, 'owner', 'user', 'original-owner'); bind(service, 'admins', 'group');
  const id = service.store.enqueue('通知🙂', ['owner', 'admins'], 'same-key');
  assert.equal(service.store.enqueue('通知🙂', ['admins', 'owner', 'owner'], 'same-key'), id);
  assert.throws(() => service.store.enqueue('changed', ['owner'], 'same-key'), /idempotency_conflict/);
  bind(service, 'owner', 'user', 'new-owner');
  await service.tick(); await service.tick();
  assert.deepEqual(calls.map(call => call.target), [{ scope: 'group', targetId: 'admins' }, { scope: 'c2c', targetId: 'original-owner' }]);
  assert.equal(service.store.notification(id).status, 'sent');
  assert.equal((await api(service, '/api/notifications/' + id)).body.status, 'sent');
  assert.equal((await api(service, '/api/notifications/' + '0'.repeat(40))).status, 404);
});

test('individual failures retry five times and manual retry leaves sent targets alone', async t => {
  const { service, bot, calls, advance } = fixture(t);
  bind(service); bind(service, 'admins', 'group');
  const original = bot.sendText;
  bot.sendText = async (target, message) => { if (target.scope === 'group') throw { httpStatus: 429, message: 'SECRET UPSTREAM' }; return original(target, message); };
  const id = service.store.enqueue('通知', ['admins', 'owner'], 'retry');
  for (let i = 0; i < 6; i++) { await service.tick(); advance(); }
  const state = service.store.notification(id);
  assert.equal(state.status, 'failed');
  assert.equal(state.targets[0].attempts, 5);
  assert.equal(state.targets[0].last_error, 'qq_http_429');
  assert.equal(calls.length, 1);
  bot.sendText = original;
  assert.equal((await api(service, '/api/notifications/' + id + '/retry', {})).body.retried_targets, 1);
  await service.tick();
  assert.equal(calls.length, 2);
  assert.equal(service.store.notification(id).status, 'sent');
});

test('missing QQ receipt never counts as successful delivery', async t => {
  const { service, bot } = fixture(t); bind(service);
  bot.sendText = async () => ({});
  const id = service.store.enqueue('通知', ['owner'], 'missing');
  await service.tick();
  assert.equal(service.store.notification(id).status, 'queued');
});

test('inbound metadata, user/group allowlists, bot filtering, deduplication and rate limits', t => {
  const { service } = fixture(t);
  assert.equal(service.ingest(incoming('通过评论 2', 'x', 'c2c', 'stranger')), false);
  assert.equal(service.ingest(incoming('通过评论 2', 'x', 'group', 'owner', 'other-group')), false);
  assert.equal(service.ingest({ ...incoming('通过评论 2'), senderIsBot: true }), false);
  assert.equal(service.ingest({ ...incoming('通过评论 2'), rawEventType: 'GROUP_MESSAGE_CREATE' }), false);
  assert.equal(service.ingest({ ...incoming('通过评论 2'), senderId: 123 }), false);
  assert.equal(service.ingest(incoming('sender=owner 通过评论 2', 'x', 'c2c', 'stranger')), false);
  assert.equal(service.ingest(incoming('<@!12345> 通过评论 2', 'g', 'group')), true);
  assert.equal(service.ingest(incoming('通过评论 2', 'g', 'group')), false);
  for (let i = 0; i < 9; i++) assert.equal(service.ingest(incoming('帮助', 'help-' + i)), true);
  assert.equal(service.ingest(incoming('帮助', 'too-many')), false);
  assert.equal(service.ingest(incoming('我的ID', 'id', 'c2c', 'stranger')), true);
});

test('notifications-only deployments ignore admin commands but permit binding and identity', t => {
  const { service } = fixture(t, { env: { ENHANCEMENT_COMMAND_URL: '', ENHANCEMENT_COMMAND_SECRET: '', QQ_ALLOWED_USERS: '' } });
  assert.equal(service.ingest(incoming('博客状态')), false);
  assert.equal(service.ingest(incoming('我的ID')), true);
});

test('signed commands preserve identity and stable body across retries', async () => {
  const config = settings({ ...environment, ...admin });
  const event = { scope: 'group', targetId: 'admins', sender: 'owner', messageId: 'stable' };
  const parsed = parseCommand('通过评论 2');
  let first;
  const fetcher = async (url, options) => {
    assert.equal(url, config.commandUrl);
    assert.equal(options.redirect, 'error');
    assert.equal(options.headers['X-Enhancement-Signature'], createHmac('sha256', config.commandSecret).update(options.headers['X-Enhancement-Timestamp'] + '.' + options.body).digest('hex'));
    if (first) assert.equal(options.body, first); else first = options.body;
    const payload = JSON.parse(options.body);
    assert.equal(payload.sender, 'owner');
    return Response.json({ ok: true, request_id: payload.request_id, result: { id: 2, status: 'approved' } });
  };
  assert.match(await invokeCommand(config, event, parsed, fetcher, 100000), /approved/);
  await invokeCommand(config, event, parsed, fetcher, 200000);
  assert.notEqual(commandBody({ ...event, sender: 'other' }, parsed, config.appId), first);
  assert.match(await invokeCommand(config, event, parsed, async () => new Response('', { status: 409 })), /勿重复/);
  await assert.rejects(invokeCommand(config, event, parsed, async () => Response.json({ ok: true, request_id: 'wrong', result: {} })), /invalid_command_receipt/);
  await assert.rejects(invokeCommand(config, event, parsed, async () => new Response('', { status: 503 })), /command_unavailable/);
});

test('saved command result survives QQ reply failure and service restart without repeating operation', async t => {
  let executed = 0;
  const dependencies = { fetcher: async (_url, options) => { executed++; return Response.json({ ok: true, request_id: JSON.parse(options.body).request_id, result: { id: 2, status: 'approved' } }); } };
  const { service, bot, config, advance, now } = fixture(t, { dependencies });
  bot.sendText = async () => { throw new Error('disconnected'); };
  assert.equal(service.ingest(incoming('通过评论 2')), true);
  await service.tick();
  assert.equal(executed, 1);
  service.store.close();
  const { Store } = await import('../src/store.js');
  service.store = new Store(config, now);
  advance();
  bot.sendText = async () => ({ id: 'done' });
  await service.tick();
  assert.equal(executed, 1);
  assert.equal(service.store.db.prepare('SELECT status FROM inbox').get().status, 2);
  assert.equal(service.ingest(incoming('通过评论 2')), false);
  assert.throws(() => new Store({ ...config, appId: 'other-app' }), /different QQ AppID/);
});

test('command network failures retry the same operation body, then report uncertainty', async t => {
  const bodies = [];
  const { service, calls, advance } = fixture(t, { dependencies: { fetcher: async (_url, options) => { bodies.push(options.body); throw new Error('network'); } } });
  service.ingest(incoming('回复评论 2：谢谢'));
  for (let i = 0; i < 5; i++) { await service.tick(); advance(); }
  assert.equal(bodies.length, 5);
  assert.equal(new Set(bodies).size, 1);
  assert.match(calls[0].message, /后台检查/);
});

test('pending notifications survive reopening the database and retention cleanup', async t => {
  const { service, config, now, advance, calls } = fixture(t);
  const { Store } = await import('../src/store.js');
  bind(service);
  const id = service.store.enqueue('等待重启', ['owner'], 'persistent');
  service.connected = false;
  await service.tick();
  assert.equal(calls.length, 0);
  service.store.close();
  service.store = new Store(config, now);
  service.store.db.prepare('UPDATE notifications SET created=?').run(now() - 31 * 86400000);
  service.store.prune();
  assert.equal(service.store.notification(id).status, 'queued');
  service.connected = true;
  await service.tick();
  assert.equal(calls[0].message, '等待重启');
  service.store.prune();
  assert.throws(() => service.store.notification(id), /notification_not_found/);
  advance();
});

test('commands admitted at the same millisecond retain arrival order', async t => {
  const { service, calls } = fixture(t);
  service.ingest(incoming('帮助', 'first'));
  service.ingest(incoming('我的ID', 'second'));
  await service.tick(); await service.tick();
  assert.deepEqual(calls.map(row => row.target.msgId), ['first', 'second']);
});

test('real Tencent SDK connects over WebSocket, normalizes official events, initializes token and sends both message types', async t => {
  const requests = [];
  let gateway;
  const server = createServer(async (req, res) => {
    const chunks = [];
    for await (const chunk of req) chunks.push(chunk);
    const body = chunks.length ? JSON.parse(Buffer.concat(chunks).toString()) : {};
    requests.push({ path: req.url, body, auth: req.headers.authorization });
    res.setHeader('Content-Type', 'application/json');
    if (req.url === '/app/getAppAccessToken') res.end(JSON.stringify({ access_token: 'fake-official-token', expires_in: 7200 }));
    else if (req.url === '/gateway') res.end(JSON.stringify({ url: gateway }));
    else res.end(JSON.stringify({ id: 'sdk-receipt-' + requests.length }));
  });
  const websocket = new WebSocketServer({ server });
  websocket.on('connection', socket => {
    socket.send(JSON.stringify({ op: 10, d: { heartbeat_interval: 60000 } }));
    socket.on('message', bytes => {
      const frame = JSON.parse(bytes.toString());
      if (frame.op === 2) {
        socket.send(JSON.stringify({ op: 0, t: 'READY', s: 1, d: { session_id: 'fake-session', user: { id: 'bot' } } }));
        socket.send(JSON.stringify({ op: 0, t: 'C2C_MESSAGE_CREATE', s: 2, d: { id: 'official-event', content: '我的ID', author: { user_openid: 'owner' }, timestamp: new Date().toISOString() } }));
      } else if (frame.op === 1) socket.send(JSON.stringify({ op: 11 }));
    });
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const url = `http://127.0.0.1:${server.address().port}`;
  gateway = url.replace('http:', 'ws:');
  const bot = new QQBot({ appId: environment.QQ_APP_ID, appSecret: environment.QQ_APP_SECRET, baseUrl: url, tokenBaseUrl: url, transport: 'websocket' });
  const { service } = fixture(t);
  service.bot = bot;
  let received;
  const inbound = new Promise(resolve => { bot.on('message', (_ctx, message) => { received = message; service.ingest(message); resolve(); }); });
  bot.on('ready', () => { service.connected = true; });
  const running = bot.start();
  t.after(async () => { bot.stop(); await running; for (const client of websocket.clients) client.terminate(); await new Promise(resolve => websocket.close(resolve)); await new Promise(resolve => server.close(resolve)); });
  await Promise.race([inbound, new Promise((_, reject) => { const timer = setTimeout(() => reject(new Error('SDK event timeout')), 5000); timer.unref(); })]);
  assert.equal(received.replyTarget.targetId, 'owner');
  await service.tick();
  await bot.sendText({ scope: 'group', targetId: 'admins' }, '主动通知');
  const reply = requests.find(row => row.path === '/v2/users/owner/messages');
  const proactive = requests.find(row => row.path === '/v2/groups/admins/messages');
  assert.equal(reply.body.msg_id, 'official-event');
  assert.match(reply.body.content, /owner/);
  assert.equal(proactive.body.msg_id, undefined);
  assert.equal(proactive.body.content, '主动通知');
  assert.equal(reply.auth, 'QQBot fake-official-token');
  assert.equal(requests.filter(row => row.path === '/app/getAppAccessToken').length, 1);
});
