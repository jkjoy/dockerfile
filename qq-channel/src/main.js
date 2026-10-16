import { QQBot } from '@tencent-connect/qqbot-nodejs';
import { settings } from './config.js';
import { ChannelService } from './service.js';

const config = settings();
const bot = new QQBot({ appId: config.appId, appSecret: config.appSecret, transport: 'websocket' });
const service = new ChannelService(config, bot);
bot.on('ready', () => { service.connected = true; console.log('QQ channel connected'); });
bot.on('resumed', () => { service.connected = true; console.log('QQ channel resumed'); });
bot.on('disconnected', () => { service.connected = false; console.warn('QQ channel disconnected; SDK will reconnect'); });
bot.on('error', () => console.error('QQ SDK error; check credentials, permissions and connectivity'));
bot.on('message', async (_context, message) => {
  try { service.ingest(message); } catch { console.error('QQ message could not be saved'); }
});
await service.listen();
service.startWorker();
console.log('QQ channel API listening on port ' + config.port);
let stopping = false;
async function shutdown(code = 0) {
  if (stopping) return;
  stopping = true;
  const timeout = setTimeout(() => process.exit(code || 1), 15000).unref();
  bot.stop();
  await service.close();
  clearTimeout(timeout);
  process.exit(code);
}
process.on('SIGTERM', () => shutdown());
process.on('SIGINT', () => shutdown());
// start() owns the long-lived gateway loop; run it alongside the HTTP server.
bot.start().catch(() => { console.error('QQ gateway failed to start'); shutdown(1); });
