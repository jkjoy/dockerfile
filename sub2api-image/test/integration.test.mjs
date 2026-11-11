import test from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { DatabaseSync } from 'node:sqlite';
import { mkdtemp, rm, stat, writeFile, mkdir } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, basename } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { createApp } from '../server.mjs';

const apiKey = 'sk-test-secret-not-for-the-browser';
const adminUsername = 'studioadmin';
const adminPassword = 'admin-test-password';
const userPassword = 'user-test-password';
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64');

async function listen(server) {
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  return `http://127.0.0.1:${server.address().port}`;
}

async function stop(server) {
  server.closeIdleConnections?.();
  await new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
}

async function fixture(t, options = {}) {
  const dataDir = await mkdtemp(join(tmpdir(), 'sub2api-image-test-'));
  const requests = [];
  let releaseHeld;
  const held = new Promise((resolve) => { releaseHeld = resolve; });
  let upstreamUrl;
  const upstream = createServer(async (req, res) => {
    try {
      const chunks = [];
      for await (const chunk of req) chunks.push(chunk);
      const raw = Buffer.concat(chunks).toString();
      const body = raw ? JSON.parse(raw) : null;
      requests.push({ path: req.url, method: req.method, authorization: req.headers.authorization, body });
      if (req.url === '/redirect.png') {
        res.writeHead(302, { location: `${upstreamUrl}/result.png` });
        res.end();
        return;
      }
      if (req.url === '/result.png') {
        res.writeHead(200, { 'content-type': 'image/png' });
        res.end(png);
        return;
      }
      res.setHeader('content-type', 'application/json');
      if (req.url !== '/v1/images/generations') {
        res.writeHead(404);
        res.end(JSON.stringify({ error: { message: `Unexpected path ${req.url}` } }));
        return;
      }
      if (body.prompt.startsWith('hold ')) await held;
      if (body.prompt === 'fail with HTTP 200') {
        res.end(`  \n${JSON.stringify({ error: { message: 'No available image accounts' } })}`);
        return;
      }
      if (body.prompt === 'return a remote URL') {
        res.end(JSON.stringify({ data: [{ url: `${upstreamUrl}/redirect.png`, revised_prompt: 'Revised remote prompt' }] }));
        return;
      }
      if (body.prompt === 'return one good image and one failed download') {
        res.end(JSON.stringify({ data: [{ b64_json: png.toString('base64') }, { url: `${upstreamUrl}/missing-image.png` }] }));
        return;
      }
      const count = body.prompt === 'return fewer than requested' ? 1 : body.n || 1;
      res.end(` \n${JSON.stringify({ data: Array.from({ length: count }, () => ({ b64_json: png.toString('base64'), revised_prompt: 'Revised image prompt' })) })}`);
    } catch (error) {
      res.statusCode = 500;
      res.end(JSON.stringify({ error: { message: error.message } }));
    }
  });
  upstreamUrl = await listen(upstream);
  const { setupData, ...appOptions } = options;
  await setupData?.({ dataDir, upstreamUrl });
  let service;
  let appServer;
  let appUrl;
  const bootOptions = { dataDir, baseUrl: upstreamUrl, apiKey, model: 'gpt-image-2', trustProxy: true, adminUsername, adminPassword, ...appOptions };
  const boot = async (config) => {
    service = await createApp(config);
    appServer = createServer(service.app);
    appUrl = await listen(appServer);
  };
  await boot(bootOptions);
  t.after(async () => {
    releaseHeld();
    try {
      await stop(appServer);
      await service.close();
      await stop(upstream);
    } finally {
      await rm(dataDir, { recursive: true, force: true });
    }
  });

  function client(ip = '203.0.113.1') {
    const cookies = new Map();
    const request = async (path, init = {}) => {
      const response = await fetch(`${appUrl}${path}`, {
        ...init,
        headers: { 'content-type': 'application/json', 'x-forwarded-for': ip, ...(cookies.size ? { cookie: [...cookies].map(([name, value]) => `${name}=${value}`).join('; ') } : {}), ...init.headers },
        body: init.body === undefined ? undefined : JSON.stringify(init.body),
      });
      for (const header of response.headers.getSetCookie()) {
        const [pair] = header.split(';');
        const index = pair.indexOf('=');
        const name = pair.slice(0, index);
        const value = pair.slice(index + 1);
        if (!value || /max-age=0/i.test(header)) cookies.delete(name);
        else cookies.set(name, value);
      }
      return response;
    };
    const me = async () => {
      const response = await request('/api/me');
      assert.equal(response.status, 200, await response.clone().text());
      return response.json();
    };
    const register = async (username = 'alice', extra = {}) => {
      const response = await request('/api/auth/register', { method: 'POST', body: { username, password: userPassword, ...extra } });
      assert.ok([200, 201].includes(response.status), await response.clone().text());
      return response.json();
    };
    const login = async (username = adminUsername, password = adminPassword) => {
      const response = await request('/api/auth/login', { method: 'POST', body: { username, password } });
      assert.equal(response.status, 200, await response.clone().text());
      return response.json();
    };
    const generate = async (body = {}) => {
      const response = await request('/api/generations', { method: 'POST', body: { prompt: 'A quiet forest', size: '1024x1024', quality: 'auto', n: 1, background: 'auto', ...body } });
      assert.equal(response.status, 202, await response.clone().text());
      const result = await response.json();
      assert.ok(result.generation.id);
      return result.generation;
    };
    const completed = async (id) => {
      const until = Date.now() + 8_000;
      while (Date.now() < until) {
        const response = await request(`/api/generations/${id}`);
        assert.equal(response.status, 200, await response.clone().text());
        const { generation } = await response.json();
        if (['completed', 'failed'].includes(generation.status)) return generation;
        await delay(15);
      }
      throw new Error(`Generation ${id} did not finish within 8 seconds`);
    };
    return { request, me, register, login, generate, completed, cookies };
  }
  return {
    dataDir, requests, upstreamUrl, client, releaseHeld,
    restart: async (changes = {}) => {
      await stop(appServer);
      await service.close();
      await boot({ ...bootOptions, ...changes });
    },
  };
}

test('configuration uses server environment only, ignores legacy settings, and never exposes upstream secrets', async (t) => {
  const f = await fixture(t, {
    setupData: async ({ dataDir }) => writeFile(join(dataDir, 'config.json'), JSON.stringify({ baseUrl: 'https://old.invalid', apiKey: 'sk-legacy-unused', model: 'old-model' })),
  });
  const c = f.client();
  const config = await (await c.request('/api/config')).json();
  assert.deepEqual(Object.keys(config).sort(), ['basicMode', 'configured', 'model', 'quotaTimezone', 'timeoutSeconds'].sort());
  assert.equal(config.configured, true);
  assert.equal(config.model, 'gpt-image-2');
  assert.equal(config.quotaTimezone, 'Asia/Shanghai');
  assert.equal(JSON.stringify(config).includes(apiKey), false);
  assert.equal(JSON.stringify(config).includes(f.upstreamUrl), false);
  const update = await c.request('/api/config', { method: 'PUT', body: { baseUrl: 'https://malicious.invalid', apiKey: 'stolen', model: 'different-model' } });
  assert.ok([404, 405].includes(update.status), await update.clone().text());
  const job = await c.generate({ model: 'client-cannot-select-this-model', basicMode: true });
  const done = await c.completed(job.id);
  assert.equal(done.model, 'gpt-image-2');
  assert.equal(done.basicMode, false);
  const upstream = f.requests.find((request) => request.method === 'POST');
  assert.equal(upstream.body.model, 'gpt-image-2');
  assert.equal(upstream.authorization, `Bearer ${apiKey}`);
  const savedEnvironment = Object.fromEntries(['SUB2API_BASE_URL', 'SUB2API_API_KEY', 'SUB2API_MODEL'].map((name) => [name, process.env[name]]));
  t.after(() => { for (const [name, value] of Object.entries(savedEnvironment)) { if (value === undefined) delete process.env[name]; else process.env[name] = value; } });
  process.env.SUB2API_BASE_URL = `${f.upstreamUrl}/v1/`;
  process.env.SUB2API_API_KEY = apiKey;
  process.env.SUB2API_MODEL = 'gpt-image-1';
  await f.restart({ baseUrl: undefined, apiKey: undefined, model: undefined });
  const changed = await c.generate();
  assert.equal((await c.completed(changed.id)).model, 'gpt-image-1');
  assert.equal(f.requests.filter((request) => request.method === 'POST').at(-1).path, '/v1/images/generations');
});

test('a guest can create exactly three images per day and deleting history does not restore quota', async (t) => {
  const f = await fixture(t);
  const c = f.client();
  const initial = await c.me();
  assert.equal(initial.user, null);
  assert.equal(initial.quota.tier, 'guest');
  assert.equal(initial.quota.limit, 3);
  assert.equal(initial.quota.remaining, 3);
  assert.equal(initial.quota.timeZone, 'Asia/Shanghai');
  const job = await c.generate({ n: 3 });
  assert.equal((await c.completed(job.id)).images.length, 3);
  const used = await c.me();
  assert.equal(used.quota.used, 3);
  assert.equal(used.quota.reserved, 0);
  assert.equal(used.quota.remaining, 0);
  assert.equal((await c.request(`/api/generations/${job.id}`, { method: 'DELETE' })).status, 204);
  assert.equal((await c.me()).quota.used, 3);
  assert.equal((await c.request('/api/generations', { method: 'POST', body: { prompt: 'one too many' } })).status, 429);
  assert.equal(f.requests.filter((request) => request.method === 'POST').length, 1);
});

test('guests share quota by IP but history and images remain scoped to each browser', async (t) => {
  const f = await fixture(t);
  const first = f.client('203.0.113.22');
  const second = f.client('203.0.113.22');
  const otherIp = f.client('203.0.113.23');
  await first.me();
  await second.me();
  const job = await first.generate({ n: 2 });
  const done = await first.completed(job.id);
  assert.equal((await second.me()).quota.remaining, 1);
  assert.equal((await otherIp.me()).quota.remaining, 3);
  assert.equal((await (await second.request('/api/generations')).json()).total, 0);
  assert.equal((await second.request(`/api/generations/${job.id}`)).status, 404);
  assert.equal((await second.request(`/api/generations/${job.id}`, { method: 'DELETE' })).status, 404);
  assert.equal((await second.request(done.images[0].url)).status, 404);
  assert.equal((await second.request(done.images[0].downloadUrl)).status, 404);
  const own = await second.generate();
  await second.completed(own.id);
  assert.equal((await first.me()).quota.remaining, 0);
});

test('untrusted forwarded headers cannot create fresh visitor quota', async (t) => {
  const f = await fixture(t, { trustProxy: false });
  const first = f.client('203.0.113.31');
  const spoofed = f.client('198.51.100.91');
  const job = await first.generate({ n: 3 });
  await first.completed(job.id);
  assert.equal((await spoofed.me()).quota.remaining, 0);
  assert.equal((await spoofed.request('/api/generations', { method: 'POST', body: { prompt: 'spoofed ip' } })).status, 429);
});

test('registration ignores forged privileges and login sessions survive restart', async (t) => {
  const f = await fixture(t);
  const c = f.client();
  const registered = await c.register('Alice', { role: 'admin', tier: 'paid' });
  assert.equal(registered.user.username, 'alice');
  assert.equal(registered.user.role, 'user');
  assert.equal(registered.user.tier, 'registered');
  assert.equal(registered.quota.limit, 10);
  assert.equal(JSON.stringify(registered).includes(userPassword), false);
  const duplicate = await f.client('203.0.113.55').request('/api/auth/register', { method: 'POST', body: { username: 'ALICE', password: userPassword } });
  assert.ok([400, 409].includes(duplicate.status));
  const job = await c.generate();
  await c.completed(job.id);
  await f.restart();
  assert.equal((await c.me()).user.id, registered.user.id);
  assert.equal((await c.me()).quota.used, 1);
  const logout = await c.request('/api/auth/logout', { method: 'POST' });
  assert.equal(logout.status, 200);
  assert.equal((await logout.json()).user, null);
  assert.equal((await c.me()).user, null);
  assert.equal((await c.request(`/api/generations/${job.id}`)).status, 404);
  const badLogin = await c.request('/api/auth/login', { method: 'POST', body: { username: 'alice', password: 'wrong-password' } });
  assert.equal(badLogin.status, 401);
  assert.equal((await c.login('ALICE', userPassword)).user.id, registered.user.id);
  assert.equal((await c.request(`/api/generations/${job.id}`)).status, 200);
});

test('accounts have isolated history and cannot read or delete other users images', async (t) => {
  const f = await fixture(t);
  const alice = f.client('203.0.113.61');
  const bob = f.client('203.0.113.62');
  await alice.register('alice');
  await bob.register('bob');
  const job = await alice.generate();
  const done = await alice.completed(job.id);
  for (const path of [`/api/generations/${job.id}`, done.images[0].url, done.images[0].downloadUrl]) assert.equal((await bob.request(path)).status, 404, path);
  assert.equal((await bob.request(`/api/generations/${job.id}`, { method: 'DELETE' })).status, 404);
  assert.equal((await (await bob.request('/api/generations')).json()).total, 0);
  assert.equal((await bob.me()).quota.used, 0);
  assert.equal((await alice.me()).quota.used, 1);
});

test('only administrators may upgrade accounts to 100 daily images; downgrades preserve usage', async (t) => {
  const f = await fixture(t);
  const admin = f.client('203.0.113.71');
  const user = f.client('203.0.113.72');
  const guest = f.client('203.0.113.73');
  const account = await user.register('creator');
  const adminMe = await admin.login();
  assert.equal(adminMe.user.role, 'admin');
  assert.equal(adminMe.user.tier, 'paid');
  assert.equal(adminMe.quota.limit, 100);
  assert.equal((await guest.request('/api/admin/users')).status, 401);
  assert.equal((await user.request('/api/admin/users')).status, 403);
  assert.equal((await user.request(`/api/admin/users/${account.user.id}`, { method: 'PATCH', body: { tier: 'paid' } })).status, 403);
  const usersResponse = await admin.request('/api/admin/users?page=1&limit=1');
  assert.equal(usersResponse.status, 200);
  const users = await usersResponse.json();
  assert.equal(users.total, 2);
  assert.equal(users.items.length, 1);
  assert.equal(JSON.stringify(users).includes(userPassword), false);
  assert.equal(JSON.stringify(users).includes(adminPassword), false);
  const upgrade = await admin.request(`/api/admin/users/${account.user.id}`, { method: 'PATCH', body: { tier: 'paid' } });
  assert.equal(upgrade.status, 200);
  assert.equal((await upgrade.json()).quota.limit, 100);
  assert.equal((await user.me()).quota.limit, 100);
  for (let index = 0; index < 25; index++) {
    const job = await user.generate({ prompt: `Paid batch ${index}`, n: 4 });
    assert.equal((await user.completed(job.id)).images.length, 4);
  }
  assert.equal((await user.me()).quota.used, 100);
  assert.equal((await user.request('/api/generations', { method: 'POST', body: { prompt: '101st paid image' } })).status, 429);
  const downgrade = await admin.request(`/api/admin/users/${account.user.id}`, { method: 'PATCH', body: { tier: 'registered', role: 'admin' } });
  assert.equal(downgrade.status, 200);
  const downgraded = await downgrade.json();
  assert.equal(downgraded.user.tier, 'registered');
  assert.equal(downgraded.user.role, 'user');
  assert.equal(downgraded.quota.limit, 10);
  assert.equal(downgraded.quota.used, 100);
  assert.equal(downgraded.quota.remaining, 0);
  assert.equal((await user.request('/api/generations', { method: 'POST', body: { prompt: 'over downgraded limit' } })).status, 429);
});

test('registered accounts get ten daily images and quotas count images rather than requests', async (t) => {
  const f = await fixture(t);
  const c = f.client();
  await c.register();
  for (const n of [4, 4, 2]) {
    const job = await c.generate({ n });
    assert.equal((await c.completed(job.id)).images.length, n);
  }
  const me = await c.me();
  assert.equal(me.quota.limit, 10);
  assert.equal(me.quota.used, 10);
  assert.equal(me.quota.remaining, 0);
  assert.equal((await c.request('/api/generations', { method: 'POST', body: { prompt: 'eleventh image' } })).status, 429);
  assert.equal(f.requests.filter((request) => request.method === 'POST').length, 3);
});

test('parallel submissions reserve quota atomically before upstream requests finish', async (t) => {
  const f = await fixture(t);
  const c = f.client();
  await c.me();
  const submitted = await Promise.all(Array.from({ length: 4 }, (_, index) => c.request('/api/generations', { method: 'POST', body: { prompt: `hold concurrent request ${index}`, n: 1 } })));
  assert.equal(submitted.filter((response) => response.status === 202).length, 3);
  assert.equal(submitted.filter((response) => response.status === 429).length, 1);
  const reserved = await c.me();
  assert.equal(reserved.quota.used, 0);
  assert.equal(reserved.quota.reserved, 3);
  assert.equal(reserved.quota.remaining, 0);
  f.releaseHeld();
  for (const response of submitted.filter((item) => item.status === 202)) await c.completed((await response.json()).generation.id);
  const settled = await c.me();
  assert.equal(settled.quota.used, 3);
  assert.equal(settled.quota.reserved, 0);
});

test('HTTP 200 errors refund reservations and fewer returned images consume only their actual count', async (t) => {
  const f = await fixture(t);
  const c = f.client();
  const failedJob = await c.generate({ prompt: 'fail with HTTP 200', n: 3 });
  const failed = await c.completed(failedJob.id);
  assert.equal(failed.status, 'failed');
  assert.match(failed.error, /No available image accounts|分组|账户|上游/);
  assert.equal(failed.images.length, 0);
  assert.equal(JSON.stringify(failed).includes(apiKey), false);
  assert.equal((await c.me()).quota.remaining, 3);
  const fewer = await c.generate({ prompt: 'return fewer than requested', n: 3 });
  assert.equal((await c.completed(fewer.id)).images.length, 1);
  const me = await c.me();
  assert.equal(me.quota.used, 1);
  assert.equal(me.quota.reserved, 0);
  assert.equal(me.quota.remaining, 2);
});

test('base64 and redirected URL results persist locally without forwarding credentials to image hosts', async (t) => {
  const f = await fixture(t);
  const c = f.client();
  const first = await c.generate();
  const firstDone = await c.completed(first.id);
  const second = await c.generate({ prompt: 'return a remote URL' });
  const secondDone = await c.completed(second.id);
  for (const path of ['/redirect.png', '/result.png']) {
    const download = f.requests.find((request) => request.path === path);
    assert.ok(download, path);
    assert.equal(download.authorization, undefined, path);
  }
  for (const job of [firstDone, secondDone]) {
    assert.equal(job.status, 'completed');
    const image = job.images[0];
    const response = await c.request(image.url);
    assert.equal(response.status, 200);
    assert.deepEqual(Buffer.from(await response.arrayBuffer()), png);
    const download = await c.request(image.downloadUrl);
    assert.equal(download.status, 200);
    assert.match(download.headers.get('content-disposition'), /attachment/);
    assert.deepEqual(Buffer.from(await download.arrayBuffer()), png);
  }
  await f.restart();
  const restored = await (await c.request(`/api/generations/${first.id}`)).json();
  assert.equal(restored.generation.status, 'completed');
  assert.equal((await c.me()).quota.used, 2);
  assert.equal((await c.request(restored.generation.images[0].url)).status, 200);
});

test('server basic compatibility mode cannot be overridden by browser parameters', async (t) => {
  const f = await fixture(t, { basicMode: true });
  const c = f.client();
  const job = await c.generate({ basicMode: false, model: 'different-model', quality: 'high', background: 'transparent' });
  const done = await c.completed(job.id);
  assert.equal(done.status, 'completed');
  assert.equal(done.basicMode, true);
  const upstream = f.requests.find((request) => request.method === 'POST');
  assert.equal(upstream.body.prompt, 'A quiet forest');
  for (const field of ['model', 'size', 'quality', 'background', 'output_format']) assert.equal(Object.hasOwn(upstream.body, field), false, field);
  assert.ok(upstream.body.n === undefined || upstream.body.n === 1);
});

test('history search, pagination and deletion update owned history and remove files', async (t) => {
  const f = await fixture(t);
  const c = f.client();
  await c.register();
  const forest = await c.generate({ prompt: 'Forest in morning light' });
  const forestDone = await c.completed(forest.id);
  const ocean = await c.generate({ prompt: 'Ocean in moonlight' });
  await c.completed(ocean.id);
  const failed = await c.generate({ prompt: 'fail with HTTP 200' });
  await c.completed(failed.id);
  const filtered = await (await c.request('/api/generations?q=Forest&status=completed')).json();
  assert.equal(filtered.total, 1);
  assert.equal(filtered.items[0].id, forest.id);
  const paged = await (await c.request('/api/generations?limit=1&page=2')).json();
  assert.equal(paged.total, 3);
  assert.equal(paged.page, 2);
  assert.equal(paged.pages, 3);
  assert.equal(paged.items.length, 1);
  assert.equal(paged.stats.completed, 2);
  assert.equal(paged.stats.failed, 1);
  assert.equal(paged.stats.images, 2);
  const filePath = join(f.dataDir, 'images', basename(forestDone.images[0].url));
  assert.ok((await stat(filePath)).isFile());
  assert.equal((await c.request(`/api/generations/${forest.id}`, { method: 'DELETE' })).status, 204);
  assert.equal((await c.request(`/api/generations/${forest.id}`)).status, 404);
  assert.equal((await c.request(forestDone.images[0].url)).status, 404);
  await assert.rejects(stat(filePath), { code: 'ENOENT' });
  assert.equal((await c.me()).quota.used, 2);
});

test('invalid authentication and generation inputs and cross-site mutations are rejected', async (t) => {
  const f = await fixture(t);
  const c = f.client();
  for (const body of [{ username: 'ab', password: userPassword }, { username: 'has space', password: userPassword }, { username: '用户名', password: userPassword }, { username: 'valid_name', password: 'short' }]) {
    const response = await c.request('/api/auth/register', { method: 'POST', body });
    assert.equal(response.status, 400, await response.clone().text());
  }
  for (const body of [{ prompt: '' }, { prompt: 'hello', n: 0 }, { prompt: 'hello', n: 5 }, { prompt: 'hello', size: 'malformed' }]) {
    const response = await c.request('/api/generations', { method: 'POST', body });
    assert.equal(response.status, 400, await response.clone().text());
  }
  for (const path of ['/api/auth/register', '/api/auth/login', '/api/generations']) {
    const crossed = await c.request(path, { method: 'POST', headers: { origin: 'https://evil.example' }, body: { username: 'valid_name', password: userPassword, prompt: 'cross-site image' } });
    assert.equal(crossed.status, 403, path);
  }
  assert.equal(f.requests.length, 0);
});

test('partial failures preserve successful images and only charge saved images', async (t) => {
  const f = await fixture(t);
  const c = f.client();
  const job = await c.generate({ prompt: 'return one good image and one failed download', n: 2 });
  const partial = await c.completed(job.id);
  assert.equal(partial.status, 'failed');
  assert.equal(partial.images.length, 1);
  assert.ok(partial.error);
  const saved = await c.request(partial.images[0].url);
  assert.equal(saved.status, 200);
  assert.deepEqual(Buffer.from(await saved.arrayBuffer()), png);
  const history = await (await c.request('/api/generations')).json();
  assert.equal(history.stats.failed, 1);
  assert.equal(history.stats.images, 1);
  const me = await c.me();
  assert.equal(me.quota.used, 1);
  assert.equal(me.quota.reserved, 0);
  assert.equal(me.quota.remaining, 2);
  await f.restart();
  const restored = await (await c.request(`/api/generations/${job.id}`)).json();
  assert.equal(restored.generation.images.length, 1);
  assert.equal((await c.me()).quota.used, 1);
});

test('restarting fails unfinished tasks and releases reservations without retrying upstream requests', async (t) => {
  const f = await fixture(t);
  const c = f.client();
  const active = await c.generate({ prompt: 'hold first request', n: 2 });
  const queued = await c.generate({ prompt: 'queued request', n: 1 });
  assert.equal((await c.me()).quota.reserved, 3);
  assert.equal((await c.request(`/api/generations/${active.id}`, { method: 'DELETE' })).status, 409);
  const until = Date.now() + 2_000;
  while (!f.requests.some((request) => request.method === 'POST') && Date.now() < until) await delay(10);
  await f.restart();
  for (const job of [active, queued]) assert.equal((await (await c.request(`/api/generations/${job.id}`)).json()).generation.status, 'failed');
  const me = await c.me();
  assert.equal(me.quota.reserved, 0);
  assert.equal(me.quota.used, 0);
  assert.equal(me.quota.remaining, 3);
  assert.equal(f.requests.filter((request) => request.method === 'POST').length, 1);
  f.releaseHeld();
});

test('Beijing midnight resets quota while pending work settles against its submission day', async (t) => {
  let now = new Date('2026-11-10T15:59:00.000Z');
  const f = await fixture(t, { now: () => now });
  const c = f.client();
  const initial = await c.me();
  assert.equal(initial.quota.day, '2026-11-10');
  assert.equal(new Date(initial.quota.resetAt).toISOString(), '2026-11-10T16:00:00.000Z');
  const oldDayJob = await c.generate({ prompt: 'hold old day request', n: 2 });
  assert.equal((await c.me()).quota.reserved, 2);
  now = new Date('2026-11-10T16:01:00.000Z');
  const nextDay = await c.me();
  assert.equal(nextDay.quota.day, '2026-11-11');
  assert.equal(nextDay.quota.used, 0);
  assert.equal(nextDay.quota.reserved, 0);
  assert.equal(nextDay.quota.remaining, 3);
  const newDayJob = await c.generate({ prompt: 'new day request', n: 3 });
  f.releaseHeld();
  await c.completed(oldDayJob.id);
  await c.completed(newDayJob.id);
  assert.equal((await c.me()).quota.used, 3);
  now = new Date('2026-11-10T15:59:30.000Z');
  const previousDay = await c.me();
  assert.equal(previousDay.quota.used, 2);
  assert.equal(previousDay.quota.reserved, 0);
});

test('legacy shared history is visible only to an administrator after migration', async (t) => {
  const legacyId = '11111111-1111-4111-8111-111111111111';
  const fileName = '22222222-2222-4222-8222-222222222222.png';
  const f = await fixture(t, {
    setupData: async ({ dataDir }) => {
      await mkdir(join(dataDir, 'images'), { recursive: true });
      await writeFile(join(dataDir, 'images', fileName), png);
      const db = new DatabaseSync(join(dataDir, 'history.sqlite'));
      db.exec(`CREATE TABLE generations (id TEXT PRIMARY KEY,prompt TEXT NOT NULL,model TEXT NOT NULL,size TEXT NOT NULL,quality TEXT NOT NULL,background TEXT NOT NULL,n INTEGER NOT NULL,basic_mode INTEGER NOT NULL DEFAULT 0,status TEXT NOT NULL,error TEXT,created_at TEXT NOT NULL,completed_at TEXT,images TEXT NOT NULL DEFAULT '[]')`);
      db.prepare('INSERT INTO generations(id,prompt,model,size,quality,background,n,status,created_at,completed_at,images) VALUES(?,?,?,?,?,?,?,?,?,?,?)').run(legacyId, 'Legacy shared landscape', 'gpt-image-2', '1024x1024', 'auto', 'auto', 1, 'completed', '2026-11-01T00:00:00.000Z', '2026-11-01T00:00:01.000Z', JSON.stringify([{ id: fileName.slice(0, -4), url: `/images/${fileName}`, downloadUrl: `/api/images/${fileName}/download` }]));
      db.close();
    },
  });
  const guest = f.client();
  const user = f.client('203.0.113.81');
  const admin = f.client('203.0.113.82');
  await user.register();
  await admin.login();
  for (const client of [guest, user]) {
    assert.equal((await (await client.request('/api/generations')).json()).total, 0);
    assert.equal((await client.request(`/api/generations/${legacyId}`)).status, 404);
    assert.equal((await client.request(`/images/${fileName}`)).status, 404);
  }
  assert.equal((await (await admin.request('/api/generations')).json()).total, 1);
  assert.equal((await admin.request(`/api/generations/${legacyId}`)).status, 200);
  assert.equal((await admin.request(`/images/${fileName}`)).status, 200);
  assert.equal((await admin.me()).quota.used, 0);
});

test('guest identity cookies cannot be cached with shared static assets', async (t) => {
  const f = await fixture(t);
  const fresh = f.client();
  const asset = await fresh.request('/vendor/phosphor/regular/style.css');
  assert.equal(asset.status, 200);
  assert.equal(asset.headers.getSetCookie().length, 0);
  const home = await fresh.request('/');
  assert.equal(home.status, 200);
  assert.ok(home.headers.getSetCookie().some((value) => value.startsWith('sub2api_guest=')));
  assert.match(home.headers.get('cache-control'), /private/);
  assert.match(home.headers.get('cache-control'), /no-store/);
});

test('a valid account session without a visitor cookie still scopes history to that account', async (t) => {
  const f = await fixture(t);
  const visitor = f.client();
  const account = f.client('203.0.113.91');
  const visitorJob = await visitor.generate({ prompt: 'Private visitor image' });
  const visitorDone = await visitor.completed(visitorJob.id);
  await account.register();
  const accountJob = await account.generate({ prompt: 'Private account image' });
  await account.completed(accountJob.id);
  account.cookies.delete('sub2api_guest');
  const history = await (await account.request('/api/generations')).json();
  assert.equal(history.total, 1);
  assert.equal(history.items[0].id, accountJob.id);
  assert.equal((await account.request(`/api/generations/${visitorJob.id}`)).status, 404);
  assert.equal((await account.request(visitorDone.images[0].url)).status, 404);
});

test('concurrent registration cannot claim the reserved administrator username', async (t) => {
  const f = await fixture(t, { adminPassword: '' });
  const attempts = await Promise.all(Array.from({ length: 3 }, (_, index) => f.client(`203.0.113.${100 + index}`).request('/api/auth/register', {
    method: 'POST', body: { username: adminUsername.toUpperCase(), password: userPassword, role: 'admin', tier: 'paid' },
  })));
  assert.ok(attempts.every((response) => response.status === 409));
  await f.restart({ adminPassword });
  const admin = f.client('203.0.113.110');
  assert.equal((await admin.login()).user.role, 'admin');
  assert.equal((await (await admin.request('/api/admin/users')).json()).total, 1);
});
