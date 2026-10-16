import { createHash, createHmac } from 'node:crypto';
import { text } from './config.js';

export const HELP = 'QQ 博客管理\n我的ID\n绑定 <口令>\n博客状态\n待审评论 [数量]\n已审评论 [数量]\n垃圾评论 [数量]\n通过评论 <编号>\n暂缓评论 <编号>\n垃圾标记 <编号>\n回复评论 <编号>：<正文>\n待审友链 [数量]\n通过友链 <编号>\n驳回友链 <编号>\n群内需 @机器人；管理操作需站长白名单。';

export function parseCommand(input) {
  const value = input.trim().replace(/^\//, '');
  if (/^(?:帮助|help)$/i.test(value)) return { local: 'help' };
  if (/^(?:我的ID|id)$/i.test(value)) return { local: 'identity' };
  const binding = /^绑定\s+([a-f0-9]{32})$/.exec(value);
  if (binding) return { local: 'bind', code: binding[1] };
  if (/^(?:博客状态|状态|status)$/i.test(value)) return { command: 'status', args: {} };
  const listing = /^(待审评论|已审评论|垃圾评论|待审友链)(?:\s+([1-9]\d?))?$/.exec(value);
  if (listing) {
    const limit = Number(listing[2] ?? 5);
    if (limit > 20) return null;
    return listing[1] === '待审友链' ? { command: 'links.list', args: { state: '0', limit } }
      : { command: 'comments.list', args: { status: { 待审评论: 'waiting', 已审评论: 'approved', 垃圾评论: 'spam' }[listing[1]], limit } };
  }
  const operation = /^(通过评论|暂缓评论|垃圾标记|通过友链|驳回友链)\s+([1-9]\d{0,9})$/.exec(value);
  const reply = /^回复评论\s+([1-9]\d{0,9})\s*[：:]\s*([\s\S]+)$/.exec(value);
  const id = Number(operation?.[2] ?? reply?.[1]);
  if (!Number.isSafeInteger(id) || id > 2147483647) return null;
  if (operation) return { command: { 通过评论: 'comment.approve', 暂缓评论: 'comment.waiting', 垃圾标记: 'comment.spam', 通过友链: 'link.approve', 驳回友链: 'link.reject' }[operation[1]], args: { id } };
  if (reply && Buffer.byteLength(reply[2]) <= 4000) return { command: 'comment.reply', args: { id, text: reply[2] } };
  return null;
}

export function commandBody(event, parsed, appId) {
  const identity = JSON.stringify([appId, event.scope, event.targetId, event.sender, event.messageId, parsed.command]);
  return JSON.stringify({ request_id: createHash('sha256').update(identity).digest('hex'),
    sender: event.sender, command: parsed.command, args: parsed.args });
}

export async function invokeCommand(config, event, parsed, fetcher = fetch, now = Date.now()) {
  const body = commandBody(event, parsed, config.appId);
  const timestamp = String(Math.floor(now / 1000));
  const signature = createHmac('sha256', config.commandSecret).update(timestamp + '.' + body).digest('hex');
  const response = await fetcher(config.commandUrl, { method: 'POST', body, redirect: 'error', signal: AbortSignal.timeout(12000), headers: {
    'Content-Type': 'application/json', 'Accept': 'application/json',
    'X-Enhancement-Timestamp': timestamp, 'X-Enhancement-Signature': signature,
  } });
  if (response.status >= 500 || response.status === 429) throw new Error('command_unavailable');
  if (!response.ok) return response.status === 409 ? '操作状态不确定，请到博客后台检查，勿重复提交。'
    : `博客拒绝该操作（HTTP ${response.status}），请检查指令开关、白名单和管理员身份。`;
  const chunks = [];
  let size = 0;
  for await (const part of response.body) {
    size += part.length;
    if (size > 65536) throw new Error('invalid_command_receipt');
    chunks.push(Buffer.from(part));
  }
  const bytes = Buffer.concat(chunks);
  const receipt = JSON.parse(bytes.toString('utf8'));
  if (receipt?.ok !== true || receipt.request_id !== JSON.parse(body).request_id || !receipt.result || typeof receipt.result !== 'object') {
    throw new Error('invalid_command_receipt');
  }
  return formatResult(parsed.command, receipt.result);
}

export function formatResult(command, result) {
  if (command === 'status') return text(`博客状态\n待审：${Number(result.comments?.waiting) || 0}\n已审：${Number(result.comments?.approved) || 0}\n垃圾：${Number(result.comments?.spam) || 0}`);
  if (command === 'comments.list') return text((result.comments ?? []).map(row => `#${row.coid} ${row.author}\n${row.text}`).join('\n\n') || '没有符合条件的评论。');
  if (command === 'links.list') return text((result.links ?? []).map(row => `#${row.lid} ${row.name}\n${row.description ?? ''}`).join('\n\n') || '没有待审友链。');
  return text(`操作完成：${command}\n编号：${result.id}\n状态：${result.status ?? result.state ?? '已处理'}`);
}
