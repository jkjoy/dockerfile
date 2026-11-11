import test from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { mkdtemp, rm, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, basename } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { createApp } from '../server.mjs';

const apiKey = 'sk-test-secret-not-for-the-browser';
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
      if (req.url === '/result.png') {
        res.writeHead(200, { 'content-type': 'image/png' });
        res.end(png);
        return;
      }
      res.setHeader('content-type', 'application/json');
      if (req.url === '/v1/models') {
        res.end(JSON.stringify({ data: [{ id: 'gpt-image-2' }, { id: 'gpt-image-1' }, { id: 'grok-imagine' }, { id: 'gpt-text-only' }] }));
        return;
      }
      if (req.url !== '/v1/images/generations') {
        res.writeHead(404);
        res.end(JSON.stringify({ error: { message: `Unexpected path ${req.url}` } }));
        return;
      }
      if (body.prompt === 'hold this request') await held;
      if (body.prompt === 'fail with HTTP 200') {
        res.end(`  \n${JSON.stringify({ error: { message: 'No available image accounts' } })}`);
        return;
      }
      if (body.prompt === 'return a remote URL') {
        res.end(JSON.stringify({ data: [{ url: `${upstreamUrl}/result.png`, revised_prompt: 'Revised remote prompt' }] }));
        return;
      }
      if (body.prompt === 'return one good image and one failed download') {
        res.end(JSON.stringify({ data: [{ b64_json: png.toString('base64') }, { url: `${upstreamUrl}/missing-image.png` }] }));
        return;
      }
      res.end(` \n${JSON.stringify({ data: [{ b64_json: png.toString('base64'), revised_prompt: 'Revised image prompt' }] })}`);
    } catch (error) {
      res.statusCode = 500;
      res.end(JSON.stringify({ error: { message: error.message } }));
    }
  });
  upstreamUrl = await listen(upstream);
  let service;
  let appServer;
  let appUrl;
  const boot = async (config) => {
    service = await createApp(config);
    appServer = createServer(service.app);
    appUrl = await listen(appServer);
  };
  await boot({ dataDir, baseUrl: upstreamUrl, apiKey, model: 'gpt-image-2', ...options });
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
  const request = (path, init = {}) => fetch(`${appUrl}${path}`, {
    ...init,
    headers: { 'content-type': 'application/json', ...init.headers },
    body: init.body === undefined ? undefined : JSON.stringify(init.body),
  });
  const generate = async (body = {}) => {
    const response = await request('/api/generations', {
      method: 'POST',
      body: { prompt: 'A quiet forest', model: 'gpt-image-2', size: '1024x1024', quality: 'auto', n: 1, background: 'auto', ...body },
    });
    assert.equal(response.status, 202, await response.clone().text());
    const result = await response.json();
    assert.ok(result.generation.id);
    return result.generation;
  };
  const completed = async (id) => {
    const until = Date.now() + 8_000;
    while (Date.now() < until) {
      const response = await request(`/api/generations/${id}`);
      assert.equal(response.status, 200);
      const { generation } = await response.json();
      if (['completed', 'failed'].includes(generation.status)) return generation;
      await delay(25);
    }
    throw new Error(`Generation ${id} did not finish within 8 seconds`);
  };
  return {
    dataDir, requests, upstreamUrl, request, generate, completed, releaseHeld,
    restart: async () => {
      await stop(appServer);
      await service.close();
      await boot({ dataDir });
    },
  };
}

test('configuration stays private, a blank key preserves credentials, and /v1 is normalized', async (t) => {
  const f = await fixture(t);
  const initial = await f.request('/api/config');
  const config = await initial.json();
  assert.equal(config.hasApiKey, true);
  assert.equal(config.configured, true);
  assert.equal(JSON.stringify(config).includes(apiKey), false);
  assert.equal(Object.hasOwn(config, 'apiKey'), false);

  const update = await f.request('/api/config', {
    method: 'PUT', body: { baseUrl: `${f.upstreamUrl}/v1/`, apiKey: '', model: 'gpt-image-2' },
  });
  assert.equal(update.status, 200, await update.clone().text());
  assert.equal((await update.text()).includes(apiKey), false);
  const job = await f.generate();
  assert.equal((await f.completed(job.id)).status, 'completed');
  const upstream = f.requests.find((request) => request.method === 'POST');
  assert.equal(upstream.path, '/v1/images/generations');
  assert.equal(upstream.authorization, `Bearer ${apiKey}`);

  await f.restart();
  const restored = await (await f.request('/api/config')).json();
  assert.equal(restored.hasApiKey, true);
  assert.equal(restored.configured, true);
  assert.equal(JSON.stringify(restored).includes(apiKey), false);
});

test('base64 output and remote URLs are saved locally and survive app restart', async (t) => {
  const f = await fixture(t);
  const first = await f.generate({ prompt: 'A quiet forest', n: 2 });
  const firstDone = await f.completed(first.id);
  assert.equal(firstDone.status, 'completed');
  assert.equal(firstDone.images.length, 1, 'Keep the actual available images even if fewer than n are returned');
  const second = await f.generate({ prompt: 'return a remote URL' });
  const secondDone = await f.completed(second.id);
  assert.equal(secondDone.status, 'completed');
  assert.equal(secondDone.images.length, 1);
  const remote = f.requests.find((request) => request.path === '/result.png');
  assert.ok(remote);
  assert.equal(remote.authorization, undefined, 'Do not send the sub2api credential to image hosts');

  for (const job of [firstDone, secondDone]) {
    const saved = job.images[0];
    assert.match(saved.url, /^\/images\//);
    const response = await f.request(saved.url);
    assert.equal(response.status, 200);
    assert.deepEqual(Buffer.from(await response.arrayBuffer()), png);
    const download = await f.request(saved.downloadUrl);
    assert.equal(download.status, 200);
    assert.match(download.headers.get('content-disposition'), /attachment/);
    assert.deepEqual(Buffer.from(await download.arrayBuffer()), png);
  }

  await f.restart();
  const restored = await (await f.request(`/api/generations/${first.id}`)).json();
  assert.equal(restored.generation.status, 'completed');
  assert.equal(restored.generation.prompt, 'A quiet forest');
  const image = await f.request(restored.generation.images[0].url);
  assert.equal(image.status, 200);
  assert.deepEqual(Buffer.from(await image.arrayBuffer()), png);
});

test('HTTP 200 with an upstream error is recorded as a failed generation', async (t) => {
  const f = await fixture(t);
  const job = await f.generate({ prompt: 'fail with HTTP 200' });
  const failed = await f.completed(job.id);
  assert.equal(failed.status, 'failed');
  assert.match(failed.error, /No available image accounts|分组|账户|上游/);
  assert.equal(failed.images.length, 0);
  assert.equal(JSON.stringify(failed).includes(apiKey), false);
  const history = await (await f.request('/api/generations?status=failed')).json();
  assert.equal(history.total, 1);
  assert.equal(history.items[0].id, job.id);
});

test('basic compatibility mode omits parameters that require native image accounts', async (t) => {
  const f = await fixture(t);
  const job = await f.generate({ prompt: 'A basic account image', basicMode: true, quality: 'high', background: 'transparent' });
  const done = await f.completed(job.id);
  assert.equal(done.status, 'completed');
  assert.equal(done.basicMode, true);
  const upstream = f.requests.find((request) => request.method === 'POST');
  assert.equal(upstream.body.prompt, 'A basic account image');
  for (const field of ['model', 'size', 'quality', 'background', 'output_format']) {
    assert.equal(Object.hasOwn(upstream.body, field), false, `${field} requires a native account and must be omitted`);
  }
  assert.ok(upstream.body.n === undefined || upstream.body.n === 1);
});

test('history supports keyword search, pagination and deletion of the saved image', async (t) => {
  const f = await fixture(t);
  const forest = await f.generate({ prompt: 'Forest in morning light' });
  const forestDone = await f.completed(forest.id);
  const ocean = await f.generate({ prompt: 'Ocean in moonlight' });
  await f.completed(ocean.id);
  const failed = await f.generate({ prompt: 'fail with HTTP 200' });
  await f.completed(failed.id);

  const filtered = await (await f.request('/api/generations?q=Forest&status=completed')).json();
  assert.equal(filtered.total, 1);
  assert.equal(filtered.items[0].id, forest.id);
  const paged = await (await f.request('/api/generations?limit=1&page=2')).json();
  assert.equal(paged.total, 3);
  assert.equal(paged.page, 2);
  assert.equal(paged.pages, 3);
  assert.equal(paged.items.length, 1);
  assert.equal(paged.stats.completed, 2);
  assert.equal(paged.stats.failed, 1);
  assert.equal(paged.stats.images, 2);

  const fileName = basename(forestDone.images[0].url);
  const filePath = join(f.dataDir, 'images', fileName);
  assert.ok((await stat(filePath)).isFile());
  const deletion = await f.request(`/api/generations/${forest.id}`, { method: 'DELETE' });
  assert.equal(deletion.status, 204);
  assert.equal((await f.request(`/api/generations/${forest.id}`)).status, 404);
  assert.equal((await f.request(forestDone.images[0].url)).status, 404);
  await assert.rejects(stat(filePath), { code: 'ENOENT' });
});

test('invalid requests and cross-site mutations are rejected before contacting the upstream', async (t) => {
  const f = await fixture(t);
  for (const body of [{ prompt: '' }, { prompt: 'hello', n: 0 }, { prompt: 'hello', n: 5 }, { prompt: 'hello', size: 'malformed' }]) {
    const response = await f.request('/api/generations', { method: 'POST', body });
    assert.equal(response.status, 400, await response.clone().text());
  }
  const crossed = await f.request('/api/config', {
    method: 'PUT', headers: { origin: 'https://evil.example' }, body: { baseUrl: f.upstreamUrl, apiKey: 'stolen' },
  });
  assert.equal(crossed.status, 403);
  assert.equal(f.requests.length, 0);
  assert.equal((await f.request('/api/generations/does-not-exist')).status, 404);
});

test('an active generation cannot be deleted while its upstream request is running', async (t) => {
  const f = await fixture(t);
  const job = await f.generate({ prompt: 'hold this request' });
  const deletion = await f.request(`/api/generations/${job.id}`, { method: 'DELETE' });
  assert.equal(deletion.status, 409);
  f.releaseHeld();
  assert.equal((await f.completed(job.id)).status, 'completed');
});

test('model discovery uses the configured credential and returns image models', async (t) => {
  const f = await fixture(t);
  const response = await f.request('/api/models');
  assert.equal(response.status, 200);
  const { models } = await response.json();
  assert.deepEqual(models.map((model) => model.id), ['gpt-image-2', 'gpt-image-1', 'grok-imagine']);
  assert.equal(f.requests[0].authorization, `Bearer ${apiKey}`);
});

test('optional Basic authentication protects the app while leaving health checks available', async (t) => {
  const password = 'studio-test-password';
  const f = await fixture(t, { authUsername: 'studio', authPassword: password });
  assert.equal((await f.request('/healthz')).status, 200);
  const unauthorized = await f.request('/api/config');
  assert.equal(unauthorized.status, 401);
  assert.match(unauthorized.headers.get('www-authenticate'), /^Basic /);
  assert.equal((await f.request('/')).status, 401);
  assert.equal((await f.request('/images/arbitrary.png')).status, 401);
  const authorized = await f.request('/api/config', {
    headers: { authorization: `Basic ${Buffer.from(`studio:${password}`).toString('base64')}` },
  });
  assert.equal(authorized.status, 200);
  assert.equal((await authorized.text()).includes(apiKey), false);
});

test('restarting preserves an interrupted task as failed and never resubmits the paid request', async (t) => {
  const f = await fixture(t);
  const job = await f.generate({ prompt: 'hold this request' });
  const until = Date.now() + 2_000;
  while (!f.requests.some((request) => request.method === 'POST') && Date.now() < until) await delay(10);
  assert.equal(f.requests.filter((request) => request.method === 'POST').length, 1);
  await f.restart();
  const response = await f.request(`/api/generations/${job.id}`);
  assert.equal(response.status, 200);
  const { generation } = await response.json();
  assert.equal(generation.status, 'failed');
  assert.ok(generation.error);
  assert.equal(f.requests.filter((request) => request.method === 'POST').length, 1);
  f.releaseHeld();
});

test('a later download failure preserves earlier images and counts them in history', async (t) => {
  const f = await fixture(t);
  const job = await f.generate({ prompt: 'return one good image and one failed download', n: 2 });
  const partial = await f.completed(job.id);
  assert.equal(partial.status, 'failed');
  assert.equal(partial.images.length, 1);
  assert.ok(partial.error);
  const saved = await f.request(partial.images[0].url);
  assert.equal(saved.status, 200);
  assert.deepEqual(Buffer.from(await saved.arrayBuffer()), png);
  const download = await f.request(partial.images[0].downloadUrl);
  assert.equal(download.status, 200);
  assert.deepEqual(Buffer.from(await download.arrayBuffer()), png);
  const history = await (await f.request('/api/generations')).json();
  assert.equal(history.stats.failed, 1);
  assert.equal(history.stats.images, 1);
  await f.restart();
  const restored = await (await f.request(`/api/generations/${job.id}`)).json();
  assert.equal(restored.generation.status, 'failed');
  assert.equal(restored.generation.images.length, 1);
});
