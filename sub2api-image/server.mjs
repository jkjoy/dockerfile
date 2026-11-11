import { existsSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createApp } from './lib/app.mjs';

export { createApp } from './lib/app.mjs';

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const rootDir = path.dirname(fileURLToPath(import.meta.url));
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
