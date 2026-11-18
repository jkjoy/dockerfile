const $ = (selector) => document.querySelector(selector);
let csrfToken;

function showError(message) {
  $('#setup-error').textContent = message;
  $('#setup-error').hidden = !message;
}

async function initialize() {
  $('#setup-loading').hidden = false;
  $('#setup-card').hidden = true;
  $('#setup-load-error').hidden = true;
  try {
    const response = await fetch('/api/me', { cache: 'no-store' });
    if (!response.ok) throw new Error('服务暂时不可用，请稍后重试。');
    const result = await response.json();
    if (!result.needsSetup) { location.replace('/'); return; }
    csrfToken = result.csrfToken;
    if (!csrfToken) throw new Error('初始化验证信息暂时无法读取，请刷新页面重试。');
    $('#setup-loading').hidden = true;
    $('#setup-card').hidden = false;
    $('#setup-submit').disabled = false;
    $('#setup-username').focus();
  } catch (error) {
    $('#setup-loading').hidden = true;
    $('#setup-load-error-message').textContent = error.message;
    $('#setup-load-error').hidden = false;
  }
}

$('#setup-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  showError('');
  if ($('#setup-password').value !== $('#setup-confirm-password').value) {
    showError('两次输入的密码不一致。');
    $('#setup-confirm-password').focus();
    return;
  }
  $('#setup-submit').disabled = true;
  $('#setup-submit span').textContent = '正在初始化…';
  try {
    const response = await fetch('/api/setup', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
      body: JSON.stringify({ username: $('#setup-username').value.trim(), password: $('#setup-password').value }),
    });
    const result = await response.json();
    if (response.status === 409) { location.replace('/'); return; }
    if (!response.ok) throw new Error(result.error || '初始化未完成，请稍后重试。');
    $('#setup-password').value = '';
    $('#setup-confirm-password').value = '';
    $('#setup-created-username').textContent = result.user.username;
    $('#setup-account-view').hidden = true;
    $('#setup-complete-view').hidden = false;
    $('#setup-card').setAttribute('aria-labelledby', 'setup-complete-title');
    $('#setup-step-account').classList.replace('active', 'done');
    $('#setup-step-account').removeAttribute('aria-current');
    $('#setup-step-done').classList.add('active');
    $('#setup-step-done').setAttribute('aria-current', 'step');
    $('#setup-enter').focus();
  } catch (error) { showError(error.message); }
  finally {
    $('#setup-submit').disabled = false;
    $('#setup-submit span').textContent = '完成初始化';
  }
});

$('#setup-enter').addEventListener('click', () => location.replace('/'));
$('#setup-retry').addEventListener('click', initialize);
await initialize();
