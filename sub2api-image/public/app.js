const $ = (selector) => document.querySelector(selector);
const escapeHtml = (value = '') => String(value).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
const icon = (name) => `<i class="ph ph-${name}" aria-hidden="true"></i>`;
const isProcessing = (item) => item && ['queued', 'generating'].includes(item.status);
const statusLabels = { queued: '排队中', generating: '生成中', completed: '已完成', failed: '未成功' };
const presets = {
  miniature: '一个藏在旧书里的微缩森林世界，蘑菇小屋与蜿蜒溪流，清晨薄雾，移轴摄影，温柔的自然光，丰富的微小细节。',
  nature: '午后的阳光穿过森林树冠，落在长满青苔的岩石与清澈溪流上，胶片摄影质感，自然色彩，宁静的氛围，细腻的光影层次。',
  product: '一瓶极简设计的香水置于浅灰色石台上，背景是一面柔和的白墙，斜射的窗光，清晰的玻璃折射，商业产品摄影，留白构图。',
};
const state = { config: null, me: null, recent: [], history: null, preview: null, previewIndex: 0, detail: null, detailIndex: 0, page: 1, view: 'studio', submitting: false, identityEpoch: 0, authMode: 'login', adminPage: 1 };
const tierLabels = { guest: '游客', registered: '注册用户', paid: '付费用户' };
let toastTimer;
let searchTimer;
let adminSearchTimer;
let historyRequest = 0;
let adminRequest = 0;
let refreshPromise = null;
let previewSignature = '';
let detailSignature = '';

async function api(path, { method = 'GET', body } = {}) {
  const response = await fetch(path, {
    method,
    headers: { 'Content-Type': 'application/json' },
    ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
  });
  if (response.status === 204) return null;
  let result;
  try { result = await response.json(); } catch { throw new Error('服务暂时不可用，请稍后重试。'); }
  if (!response.ok) {
    const message = typeof result.error === 'string' ? result.error : result.error?.message;
    throw Object.assign(new Error(message || result.message || `请求未成功（${response.status}）`), { status: response.status, quota: result.quota });
  }
  return result;
}

function toast(message, error = false) {
  clearTimeout(toastTimer);
  $('#toast span').textContent = message;
  $('#toast .ph').className = `ph ph-${error ? 'warning-circle' : 'check-circle'}`;
  $('#toast').hidden = false;
  toastTimer = setTimeout(() => { $('#toast').hidden = true; }, error ? 6500 : 3500);
}

function showError(selector, message) {
  $(selector).textContent = message || '';
  $(selector).hidden = !message;
}

function formatDate(value, full = false) {
  if (!value) return '尚未完成';
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
    ...(full ? { year: 'numeric' } : {}),
  }).format(new Date(value));
}

function formValues() {
  return {
    prompt: $('#prompt').value.trim(),
    size: $('input[name="size"]:checked')?.value || 'auto', quality: $('#quality').value,
    n: state.config?.basicMode ? 1 : Number($('#n').value),
    background: $('#background').value,
  };
}

function saveDraft() {
  try { localStorage.setItem(`image-studio-draft:${state.me?.user?.id || 'guest'}`, JSON.stringify(formValues())); } catch { /* Private browsing can disable local storage. */ }
  $('#prompt-count').textContent = $('#prompt').value.length;
}

function fillForm(item) {
  $('#prompt').value = item.prompt || '';
  const size = [...document.querySelectorAll('input[name="size"]')].find((input) => input.value === item.size);
  (size || $('input[name="size"][value="auto"]')).checked = true;
  for (const name of ['quality', 'background', 'n']) {
    const select = $(`#${name}`);
    if ([...select.options].some((option) => option.value === String(item[name]))) select.value = String(item[name]);
  }
  updateBasicMode();
  saveDraft();
}

function updateBasicMode() {
  const basic = Boolean(state.config?.basicMode);
  for (const input of document.querySelectorAll('#quality, #background, #n, input[name="size"]')) input.disabled = basic;
  if (basic) $('#n').value = '1';
  $('#basic-mode-note').hidden = !basic;
}

function renderConfig() {
  const configured = Boolean(state.config?.configured);
  $('#setup-banner').hidden = configured;
  $('#model-name').textContent = state.config?.model || '暂未配置';
  updateBasicMode();
  updateGenerateButton();
}

function restoreDraft() {
  let saved;
  try { saved = JSON.parse(localStorage.getItem(`image-studio-draft:${state.me?.user?.id || 'guest'}`) || 'null'); } catch { /* Ignore malformed local drafts. */ }
  fillForm(saved && typeof saved === 'object' ? saved : { prompt: '', size: '1024x1024', quality: 'auto', background: 'auto', n: 1 });
}

function updateGenerateButton() {
  const quota = state.me?.quota;
  const count = state.config?.basicMode ? 1 : Number($('#n').value);
  $('#generate-button').disabled = state.submitting || !state.config?.configured || !quota || count > quota.remaining;
  $('#generate-button span').textContent = state.submitting ? '正在提交…' : !state.config?.configured ? '服务暂未就绪' : !quota ? '正在读取额度…' : !quota.remaining ? (quota.reserved ? '额度正在使用中' : '今日额度已用完') : count > quota.remaining ? '剩余额度不足，请减少数量' : '开始生成';
  if (quota) $('#generation-quota-note').textContent = `今日可用 ${quota.remaining} 张 / 每日 ${quota.limit} 张 · 每次按图片张数计算`;
}

function renderQuota() {
  if (!state.me) return;
  const { user, quota } = state.me;
  const tier = user?.role === 'admin' ? '管理员' : tierLabels[quota.tier] || '游客';
  $('#quota-title').textContent = `今日还可生成 ${quota.remaining} 张图片`;
  $('#quota-description').textContent = `${tier} · 每日 ${quota.limit} 张${quota.reserved ? ` · ${quota.reserved} 张生成中` : ''} · 北京时间零点更新`;
  $('#quota-action').textContent = !user ? (state.me.needsSetup ? '创建管理员账号' : '注册领取每日 10 张') : quota.tier === 'paid' ? '查看我的额度' : '升级享每日 100 张';
  $('#quota-action').dataset.action = user ? 'account' : 'register';
  $('#sidebar-username').textContent = user?.username || '游客创作';
  $('#sidebar-status').textContent = `${tier} · 今日剩余 ${quota.remaining} 张`;
  $('#connection-label').textContent = user ? `${user.username} · ${tier}` : '登录 / 注册';
  $('#connection-dot').classList.toggle('connected', Boolean(user));
  $('#account-nav-label').textContent = user ? '我的账号' : '登录 / 注册';
  $('#admin-nav').hidden = user?.role !== 'admin';
  $('#profile-username').textContent = user?.username || '';
  $('#profile-tier').textContent = user?.role === 'admin' ? '管理员' : tier;
  $('#profile-remaining').textContent = quota.remaining;
  $('#profile-used').textContent = quota.used;
  $('#profile-reserved').textContent = quota.reserved;
  $('#upgrade-description').textContent = quota.tier === 'paid' ? '你已享有每天 100 张图片的创作额度。' : '如需升级付费用户，请联系管理员开通，每天可生成 100 张图片。';
  $('#profile-admin-link').hidden = user?.role !== 'admin';
  if ($('#account-dialog').open) {
    $('#auth-content').hidden = Boolean(user);
    $('#profile-content').hidden = !user;
    if (!user) renderAuthCopy();
  }
  updateGenerateButton();
}

function applyMe(result) {
  const changed = (state.me?.user?.id || null) !== (result.user?.id || null);
  if (changed) saveDraft();
  state.me = result;
  if (changed) {
    state.identityEpoch++;
    historyRequest++;
    adminRequest++;
    state.recent = [];
    state.history = null;
    state.preview = null;
    state.detail = null;
    state.page = 1;
    state.adminPage = 1;
    $('#detail-dialog').close();
    $('#history-grid').replaceChildren();
    $('#history-summary').textContent = '';
    $('#pagination').replaceChildren();
    $('#admin-users').replaceChildren();
    $('#admin-pagination').replaceChildren();
    $('#search').value = '';
    $('#status-filter').value = 'all';
    renderStats({ total: 0, images: 0, processing: 0 });
    renderRecent();
    renderPreview(true);
    restoreDraft();
    showError('#generation-error', '');
    if (state.view === 'admin' && result.user?.role !== 'admin') { location.hash = 'studio'; setView(); }
  }
  renderQuota();
}

async function loadMe() {
  const epoch = state.identityEpoch;
  const result = await api('/api/me');
  if (epoch === state.identityEpoch) applyMe(result);
}

function renderStats(stats) {
  if (!stats) return;
  $('#nav-count').textContent = stats.total;
  $('#stat-total').textContent = stats.total;
  $('#stat-images').textContent = stats.images;
  $('#stat-processing').textContent = stats.processing;
  $('#recent-subtitle').textContent = stats.total ? `已记录 ${stats.total} 次灵感，保存 ${stats.images} 张画面` : '每一张画面，都是灵感的切片';
}

function emptyState(title, description, action = '') {
  return `<div class="empty-state">${icon('images')}<div><h3>${escapeHtml(title)}</h3><p>${escapeHtml(description)}</p>${action}</div></div>`;
}

function imageCard(item) {
  const picture = item.images?.length
    ? `<img src="${escapeHtml(item.images[0].url)}" alt="${escapeHtml(item.prompt)}" loading="lazy" decoding="async">${item.images.length > 1 ? `<span class="card-image-count">${icon('stack')}${item.images.length}</span>` : ''}`
    : isProcessing(item) ? '<div class="spinner" aria-hidden="true"></div>' : icon('image-broken');
  return `<button class="image-card" data-generation="${escapeHtml(item.id)}" aria-label="查看创作：${escapeHtml(item.prompt)}"><div class="card-picture">${picture}</div><div class="card-body"><p class="card-prompt">${escapeHtml(item.prompt)}</p><div class="card-meta"><span class="card-model">${escapeHtml(item.basicMode ? '基础兼容模式' : item.model)}</span>${item.status === 'completed' ? `<time datetime="${escapeHtml(item.createdAt)}">${formatDate(item.createdAt)}</time>` : `<span class="card-status ${item.status === 'failed' ? 'failed' : ''}">${statusLabels[item.status] || '未知状态'}</span>`}</div></div></button>`;
}

function renderRecent() {
  $('#recent-grid').innerHTML = state.recent.length
    ? state.recent.map(imageCard).join('')
    : emptyState('第一张创作，即将诞生', '在上方写下提示词，你的作品会出现在这里。');
}

function thumbnailStrip(item, index, target) {
  if (item.images.length < 2) return '';
  return `<div class="preview-thumbs" aria-label="选择图片">${item.images.map((image, i) => `<button class="thumbnail ${i === index ? 'active' : ''}" data-${target}-image="${i}" aria-label="查看第 ${i + 1} 张图片" aria-pressed="${i === index}"><img src="${escapeHtml(image.url)}" alt="第 ${i + 1} 张图片"></button>`).join('')}</div>`;
}

function elapsedText(item) {
  const elapsed = Math.max(0, Math.floor((Date.now() - new Date(item.createdAt).getTime()) / 1000));
  return `${Math.floor(elapsed / 60)} 分 ${String(elapsed % 60).padStart(2, '0')} 秒`;
}

function processingContent(item) {
  return `<div class="canvas-empty processing"><div class="spinner" aria-hidden="true"></div><h3>${item.status === 'queued' ? '灵感已就位，正在排队' : '正在描绘你的想象'}</h3><p>${item.status === 'queued' ? '前面的创作完成后，将自动开始。' : '好画面需要一点时间。可以切换页面，任务会继续进行。'}</p><span class="elapsed" data-elapsed="${escapeHtml(item.createdAt)}">${elapsedText(item)}</span></div>`;
}

function failureContent(item) {
  return `<div class="canvas-empty error"><span class="error-illustration">${icon('cloud-warning')}</span><h3>这次画面没能完成</h3><p>${escapeHtml(item.error || '请调整参数后再试，或联系管理员。')}</p><button class="button secondary" data-reuse="${escapeHtml(item.id)}">${icon('arrow-counter-clockwise')}复用提示词</button></div>`;
}

function renderPreview(force = false) {
  const item = state.preview;
  const signature = JSON.stringify([item, state.previewIndex]);
  if (!force && previewSignature === signature) return;
  previewSignature = signature;
  $('#preview-status').textContent = item ? (statusLabels[item.status] || '') : '等待你的灵感';
  if (!item) {
    $('#preview-content').innerHTML = `<div class="canvas-empty"><div class="canvas-frame">${icon('image')}</div><h3>这里，是想象发生的地方</h3><p>从一个想法开始。<br>你的下一幅画面，正在等待被描述。</p><span class="preview-mini">${icon('sparkle')}支持多张生成 · 原图保存</span></div>`;
  } else if (item.images?.length) {
    state.previewIndex = Math.min(state.previewIndex, item.images.length - 1);
    const image = item.images[state.previewIndex];
    $('#preview-content').innerHTML = `<button class="preview-image-button" data-generation="${escapeHtml(item.id)}" aria-label="放大查看生成图片"><img src="${escapeHtml(image.url)}" alt="${escapeHtml(item.prompt)}"></button>${thumbnailStrip(item, state.previewIndex, 'preview')}`;
  } else {
    $('#preview-content').innerHTML = isProcessing(item) ? processingContent(item) : failureContent(item);
  }
  if (item?.images?.length) {
    const image = item.images[state.previewIndex];
    $('#preview-footer').innerHTML = `<span class="preview-footer-text" title="${escapeHtml(item.model)}">${escapeHtml(item.basicMode ? '基础兼容模式' : item.model)} · ${state.previewIndex + 1} / ${item.images.length}</span><div class="preview-actions"><button class="button secondary" data-reuse="${escapeHtml(item.id)}">${icon('arrow-counter-clockwise')}复用</button><a class="button primary" href="${escapeHtml(image.downloadUrl)}" download>${icon('download-simple')}下载原图</a></div>`;
  } else {
    $('#preview-footer').innerHTML = `${icon('shield-check')}<span>${isProcessing(item) ? '任务在服务器继续运行，无需保持此页面打开' : '生成的原图将自动保存到本地'}</span>`;
  }
}

function renderHistory() {
  const result = state.history;
  if (!result) return;
  const filtered = $('#search').value.trim() || $('#status-filter').value !== 'all';
  $('#history-summary').textContent = filtered ? `找到 ${result.total} 条匹配记录` : `共 ${result.total} 条记录，按创建时间倒序排列`;
  $('#history-grid').innerHTML = result.items.length ? result.items.map(imageCard).join('') : emptyState(filtered ? '还没有找到匹配的画面' : '灵感档案还是空白的', filtered ? '试着换个关键词，或切换筛选条件。' : '从第一张创作开始，慢慢积累你的灵感。');
  $('#pagination').innerHTML = result.pages > 1 ? `<button class="button secondary" data-history-page="${result.page - 1}" ${result.page <= 1 ? 'disabled' : ''}>${icon('caret-left')}上一页</button><span>${result.page} / ${result.pages}</span><button class="button secondary" data-history-page="${result.page + 1}" ${result.page >= result.pages ? 'disabled' : ''}>下一页${icon('caret-right')}</button>` : '';
}

async function loadRecent() {
  const epoch = state.identityEpoch;
  const result = await api('/api/generations?limit=4');
  if (epoch !== state.identityEpoch) return;
  state.recent = result.items;
  renderStats(result.stats);
  renderRecent();
  if (!state.preview && result.items.length) {
    state.preview = result.items.find(isProcessing) || result.items.find((item) => item.images.length) || result.items[0];
    state.previewIndex = 0;
    renderPreview();
  }
}

async function loadHistory() {
  const epoch = state.identityEpoch;
  const request = ++historyRequest;
  const params = new URLSearchParams({ q: $('#search').value.trim(), status: $('#status-filter').value, page: state.page, limit: '24' });
  const result = await api(`/api/generations?${params}`);
  if (request !== historyRequest || epoch !== state.identityEpoch) return;
  if (result.pages > 0 && state.page > result.pages) {
    state.page = result.pages;
    return loadHistory();
  }
  state.history = result;
  renderStats(result.stats);
  renderHistory();
}

async function refreshSelected() {
  const epoch = state.identityEpoch;
  const ids = new Set([state.preview?.id, $('#detail-dialog').open ? state.detail?.id : null].filter(Boolean));
  await Promise.all([...ids].map(async (id) => {
    try {
      const { generation } = await api(`/api/generations/${encodeURIComponent(id)}`);
      if (epoch !== state.identityEpoch) return;
      const previous = state.preview;
      if (state.preview?.id === id) {
        state.preview = generation;
        renderPreview();
        if (isProcessing(previous) && !isProcessing(generation)) toast(generation.status === 'completed' ? '画面已完成，原图已保存' : '这次创作未完成，可查看错误详情', generation.status === 'failed');
      }
      if (state.detail?.id === id && $('#detail-dialog').open) {
        state.detail = generation;
        renderDetail();
      }
    } catch (error) {
      if (epoch !== state.identityEpoch) return;
      // Lists remain browsable even if another browser deleted this selection.
      if (error.status !== 404) throw error;
      if (state.preview?.id === id) { state.preview = null; renderPreview(); }
      if (state.detail?.id === id) $('#detail-dialog').close();
    }
  }));
}

async function refreshAll({ quiet = false } = {}) {
  if (refreshPromise) return refreshPromise;
  refreshPromise = (async () => {
    try {
      await loadMe();
      await Promise.all([loadRecent(), refreshSelected(), ...(state.view === 'history' ? [loadHistory()] : [])]);
    } catch (error) { if (!quiet) toast(error.message, true); }
  })();
  try { await refreshPromise; } finally { refreshPromise = null; }
}

function setView() {
  state.view = location.hash === '#history' ? 'history' : location.hash === '#admin' && state.me?.user?.role === 'admin' ? 'admin' : 'studio';
  $('#studio-page').hidden = state.view !== 'studio';
  $('#history-page').hidden = state.view !== 'history';
  $('#admin-page').hidden = state.view !== 'admin';
  $('#breadcrumb-page').textContent = { studio: '创作工作台', history: '历史记录', admin: '用户管理' }[state.view];
  document.querySelectorAll('[data-page]').forEach((link) => {
    link.classList.toggle('active', link.dataset.page === state.view);
    if (link.dataset.page === state.view) link.setAttribute('aria-current', 'page'); else link.removeAttribute('aria-current');
  });
  if (state.view === 'history') loadHistory().catch((error) => toast(error.message, true));
  if (state.view === 'admin') loadUsers().catch((error) => showError('#admin-error', error.message));
}

function renderAuthCopy() {
  const register = state.authMode === 'register';
  const setup = Boolean(state.me?.needsSetup);
  $('#account-title').textContent = register ? (setup ? '创建第一个管理员账号' : '给灵感一个专属空间') : '欢迎回到映象';
  $('#account-description').textContent = setup ? '首次注册的用户将成为管理员，可管理账号和每日额度。' : '无需登录也能创作，注册后每天可生成 10 张。';
  $('#auth-submit').textContent = register ? (setup ? '创建管理员账号' : '注册并开始创作') : '登录账号';
}

function setAuthMode(mode) {
  state.authMode = mode;
  const register = mode === 'register';
  renderAuthCopy();
  $('#auth-password').autocomplete = register ? 'new-password' : 'current-password';
  $('#confirm-password-field').hidden = !register;
  $('#auth-confirm-password').required = register;
  document.querySelectorAll('[data-auth-mode]').forEach((button) => {
    const active = button.dataset.authMode === mode;
    button.classList.toggle('active', active);
    button.setAttribute('aria-pressed', String(active));
  });
  showError('#auth-error', '');
}

function openAccount(mode = 'login') {
  const loggedIn = Boolean(state.me?.user);
  $('#auth-content').hidden = loggedIn;
  $('#profile-content').hidden = !loggedIn;
  $('#auth-password').value = '';
  $('#auth-confirm-password').value = '';
  if (loggedIn) {
    $('#account-title').textContent = '我的创作账号';
    $('#account-description').textContent = '每一天，都有新的灵感额度。';
  } else {
    setAuthMode(mode);
  }
  renderQuota();
  if (!$('#account-dialog').open) $('#account-dialog').showModal();
}

async function loadUsers() {
  if (state.me?.user?.role !== 'admin') return;
  const epoch = state.identityEpoch;
  const request = ++adminRequest;
  const params = new URLSearchParams({ q: $('#admin-search').value.trim(), page: state.adminPage, limit: '20' });
  const result = await api(`/api/admin/users?${params}`);
  if (epoch !== state.identityEpoch || request !== adminRequest) return;
  if (result.pages > 0 && state.adminPage > result.pages) { state.adminPage = result.pages; return loadUsers(); }
  showError('#admin-error', '');
  $('#admin-summary').textContent = `共 ${result.total} 个账号`;
  $('#admin-users').innerHTML = result.items.length ? result.items.map((user) => `<article class="admin-user"><div class="admin-user-identity">${icon('user-circle')}<div><strong>${escapeHtml(user.username)}</strong><small>${user.role === 'admin' ? '管理员' : tierLabels[user.tier]} · 注册于 ${formatDate(user.createdAt, true)}</small></div></div><div class="admin-user-usage"><strong>${user.quota.remaining}<small> / ${user.quota.limit}</small></strong><span>今日可用 · 已生成 ${user.quota.used} 张${user.quota.reserved ? ` · 生成中 ${user.quota.reserved} 张` : ''}</span></div>${user.role === 'admin' ? '<span class="tier-badge">管理员</span>' : `<button class="button ${user.tier === 'paid' ? 'secondary' : 'primary'}" data-user-id="${escapeHtml(user.id)}" data-set-tier="${user.tier === 'paid' ? 'registered' : 'paid'}">${user.tier === 'paid' ? '降为注册用户' : '设为付费用户'}</button>`}</article>`).join('') : emptyState('没有找到这个账号', '试着使用不同的用户名搜索。');
  $('#admin-pagination').innerHTML = result.pages > 1 ? `<button class="button secondary" data-admin-page="${result.page - 1}" ${result.page <= 1 ? 'disabled' : ''}>${icon('caret-left')}上一页</button><span>${result.page} / ${result.pages}</span><button class="button secondary" data-admin-page="${result.page + 1}" ${result.page >= result.pages ? 'disabled' : ''}>下一页${icon('caret-right')}</button>` : '';
}

function renderDetail(force = false) {
  const item = state.detail;
  if (!item) return;
  const signature = JSON.stringify([item, state.detailIndex]);
  if (!force && signature === detailSignature) return;
  detailSignature = signature;
  state.detailIndex = Math.min(state.detailIndex, Math.max(0, item.images.length - 1));
  const image = item.images[state.detailIndex];
  const picture = image ? `<img src="${escapeHtml(image.url)}" alt="${escapeHtml(item.prompt)}">${thumbnailStrip(item, state.detailIndex, 'detail')}` : isProcessing(item) ? processingContent(item) : failureContent(item);
  const params = [
    ['生成模型', item.basicMode ? '上游默认（基础模式）' : item.model], ['创作状态', statusLabels[item.status]],
    ['画面尺寸', item.basicMode || item.size === 'auto' ? '上游自动' : item.size], ['画面质量', item.basicMode ? '上游自动' : item.quality],
    ['生成数量', `${item.images.length} / ${item.n} 张`], ['创建时间', formatDate(item.createdAt, true)],
  ];
  $('#detail-content').innerHTML = `<div class="detail-layout"><div class="detail-picture">${picture}</div><div class="detail-sidebar"><h3>创作提示词</h3><p class="detail-prompt">${escapeHtml(item.prompt)}</p><button class="text-button" data-action="copy-prompt">${icon('copy')}复制提示词</button><dl class="detail-params">${params.map(([label, value]) => `<div><dt>${escapeHtml(label)}</dt><dd>${escapeHtml(value)}</dd></div>`).join('')}</dl>${item.error && item.images.length ? `<p class="inline-error detail-error">${escapeHtml(item.error)}</p>` : ''}<div class="detail-actions"><button class="button secondary" data-reuse="${escapeHtml(item.id)}">${icon('arrow-counter-clockwise')}再次创作</button>${image ? `<a class="button primary" href="${escapeHtml(image.downloadUrl)}" download>${icon('download-simple')}下载原图</a>` : ''}<button class="button danger" data-delete="${escapeHtml(item.id)}" ${isProcessing(item) ? 'disabled title="请等待任务结束后删除"' : ''}>${icon('trash')}删除记录</button></div></div></div>`;
}

async function openDetail(id) {
  const epoch = state.identityEpoch;
  const { generation } = await api(`/api/generations/${encodeURIComponent(id)}`);
  if (epoch !== state.identityEpoch) return;
  state.detail = generation;
  state.detailIndex = state.preview?.id === id ? state.previewIndex : 0;
  renderDetail(true);
  if (!$('#detail-dialog').open) $('#detail-dialog').showModal();
}

async function reuseGeneration(id) {
  const epoch = state.identityEpoch;
  const item = state.detail?.id === id ? state.detail : state.preview?.id === id ? state.preview : (await api(`/api/generations/${encodeURIComponent(id)}`)).generation;
  if (epoch !== state.identityEpoch) return;
  fillForm(item);
  $('#detail-dialog').close();
  location.hash = 'studio';
  setView();
  $('#prompt').focus();
  $('#prompt').scrollIntoView({ block: 'center', behavior: 'instant' });
  toast('提示词与参数已填入，可调整后开始生成');
}

$('#generation-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  if (state.submitting) return;
  if (!state.config?.configured) { showError('#generation-error', '生图服务暂未就绪，请联系管理员。'); return; }
  const values = formValues();
  const epoch = state.identityEpoch;
  if (!values.prompt) { showError('#generation-error', '先描述一下你想要的画面。'); $('#prompt').focus(); return; }
  showError('#generation-error', '');
  state.submitting = true;
  $('#generate-button').disabled = true;
  $('#generate-button span').textContent = '正在提交…';
  try {
    const { generation, quota } = await api('/api/generations', { method: 'POST', body: values });
    if (epoch !== state.identityEpoch) return;
    if (quota && state.me) { state.me.quota = quota; renderQuota(); }
    state.preview = generation;
    state.previewIndex = 0;
    renderPreview();
    saveDraft();
    toast('创作已加入队列，完成后自动保存');
    await refreshAll({ quiet: true });
  } catch (error) {
    if (epoch !== state.identityEpoch) return;
    showError('#generation-error', error.message);
    if (error.quota && state.me) { state.me.quota = error.quota; renderQuota(); }
  }
  finally {
    state.submitting = false;
    updateGenerateButton();
  }
});

$('#auth-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  showError('#auth-error', '');
  if (state.authMode === 'register' && $('#auth-password').value !== $('#auth-confirm-password').value) {
    showError('#auth-error', '两次输入的密码不一致。');
    return;
  }
  $('#auth-submit').disabled = true;
  try {
    const result = await api(`/api/auth/${state.authMode}`, { method: 'POST', body: { username: $('#auth-username').value.trim(), password: $('#auth-password').value } });
    applyMe(result);
    $('#auth-password').value = '';
    $('#auth-confirm-password').value = '';
    $('#account-dialog').close();
    await refreshAll();
    toast(state.authMode === 'register' ? (result.user.role === 'admin' ? '管理员账号已创建，可以进入用户管理' : '注册成功，每天可以创作 10 张图片') : '登录成功，欢迎回来');
  } catch (error) { showError('#auth-error', error.message); }
  finally { $('#auth-submit').disabled = false; }
});

$('#logout-button').addEventListener('click', async () => {
  $('#logout-button').disabled = true;
  try {
    applyMe(await api('/api/auth/logout', { method: 'POST', body: {} }));
    $('#account-dialog').close();
    await refreshAll();
    toast('已退出，继续以游客身份创作');
  } catch (error) { toast(error.message, true); }
  finally { $('#logout-button').disabled = false; }
});

document.addEventListener('click', async (event) => {
  const button = event.target.closest('button, a');
  if (!button || button.disabled) return;
  try {
    if (button.dataset.action === 'account') openAccount();
    if (button.dataset.action === 'register') openAccount('register');
    if (button.dataset.authMode) setAuthMode(button.dataset.authMode);
    if (button.dataset.close) $(`#${button.dataset.close}`).close();
    if (button.id === 'profile-admin-link') $('#account-dialog').close();
    if (button.dataset.preset) { $('#prompt').value = presets[button.dataset.preset]; saveDraft(); $('#prompt').focus(); }
    if (button.dataset.generation) await openDetail(button.dataset.generation);
    if (button.dataset.reuse) await reuseGeneration(button.dataset.reuse);
    if (button.dataset.previewImage !== undefined) { state.previewIndex = Number(button.dataset.previewImage); renderPreview(); }
    if (button.dataset.detailImage !== undefined) { state.detailIndex = Number(button.dataset.detailImage); renderDetail(); }
    if (button.dataset.historyPage) {
      state.page = Number(button.dataset.historyPage);
      await loadHistory();
      $('#history-summary').scrollIntoView({ block: 'start' });
    }
    if (button.dataset.adminPage) { state.adminPage = Number(button.dataset.adminPage); await loadUsers(); }
    if (button.dataset.setTier) {
      button.disabled = true;
      try {
        await api(`/api/admin/users/${encodeURIComponent(button.dataset.userId)}`, { method: 'PATCH', body: { tier: button.dataset.setTier } });
        await loadUsers();
        toast(button.dataset.setTier === 'paid' ? '已升级为付费用户，每天可生成 100 张' : '已调整为注册用户，每天可生成 10 张');
      } catch (error) { button.disabled = false; throw error; }
    }
    if (button.dataset.action === 'copy-prompt' && state.detail) {
      if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(state.detail.prompt);
      else {
        const range = document.createRange();
        range.selectNodeContents($('.detail-prompt'));
        window.getSelection().removeAllRanges();
        window.getSelection().addRange(range);
        if (!document.execCommand('copy')) { toast('已选中提示词，请按 Ctrl+C 复制'); return; }
        window.getSelection().removeAllRanges();
      }
      toast('提示词已复制');
    }
    if (button.dataset.delete) {
      if (!confirm('删除这条创作记录及其所有本地图片？此操作无法撤销。')) return;
      button.disabled = true;
      try {
        await api(`/api/generations/${encodeURIComponent(button.dataset.delete)}`, { method: 'DELETE' });
        if (state.preview?.id === button.dataset.delete) { state.preview = null; renderPreview(); }
        state.detail = null;
        $('#detail-dialog').close();
        await refreshAll();
        toast('创作记录与图片已删除');
      } catch (error) { button.disabled = false; throw error; }
    }
  } catch (error) { toast(error.message, true); }
});

$('#clear-prompt').addEventListener('click', () => { $('#prompt').value = ''; saveDraft(); $('#prompt').focus(); });
$('#generation-form').addEventListener('input', () => { saveDraft(); updateGenerateButton(); });
$('#search').addEventListener('input', () => {
  clearTimeout(searchTimer);
  // Invalidate an older search response while waiting for the next keystroke.
  historyRequest++;
  searchTimer = setTimeout(() => { state.page = 1; loadHistory().catch((error) => toast(error.message, true)); }, 300);
});
$('#status-filter').addEventListener('change', () => { state.page = 1; loadHistory().catch((error) => toast(error.message, true)); });
$('#refresh-history').addEventListener('click', () => refreshAll());
$('#admin-search').addEventListener('input', () => {
  clearTimeout(adminSearchTimer);
  adminRequest++;
  adminSearchTimer = setTimeout(() => { state.adminPage = 1; loadUsers().catch((error) => showError('#admin-error', error.message)); }, 300);
});
$('#refresh-users').addEventListener('click', () => loadUsers().catch((error) => showError('#admin-error', error.message)));
window.addEventListener('hashchange', setView);
document.addEventListener('keydown', (event) => {
  if ((event.ctrlKey || event.metaKey) && event.key === 'Enter' && state.view === 'studio' && !$('dialog[open]')) {
    event.preventDefault();
    $('#generation-form').requestSubmit();
  }
});
document.addEventListener('error', (event) => {
  if (event.target instanceof HTMLImageElement) {
    event.target.alt = '图片暂时无法读取，请刷新重试';
    event.target.classList.add('image-load-error');
  }
}, true);
for (const dialog of document.querySelectorAll('dialog')) {
  dialog.addEventListener('click', (event) => {
    if (event.target !== dialog) return;
    const rect = dialog.getBoundingClientRect();
    if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) dialog.close();
  });
}
$('#account-dialog').addEventListener('close', () => { $('#auth-password').value = ''; $('#auth-confirm-password').value = ''; });

async function init() {
  renderPreview();
  try {
    state.config = await api('/api/config');
    renderConfig();
    await loadMe();
    restoreDraft();
  } catch (error) { $('#sidebar-status').textContent = '服务连接失败'; toast(error.message, true); }
  setView();
  await refreshAll();
}
await init();
setInterval(() => { if (!document.hidden) refreshAll({ quiet: true }); }, 4000);
setInterval(() => {
  document.querySelectorAll('[data-elapsed]').forEach((node) => { node.textContent = elapsedText({ createdAt: node.dataset.elapsed }); });
}, 1000);
document.addEventListener('visibilitychange', () => { if (!document.hidden) refreshAll({ quiet: true }); });
