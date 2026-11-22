import './themes.js';
import { vaultWorkspace } from './workspace.js';
import { Credentials, Kdbx, ProtectedValue, UnsupportedArgon2 } from './crypto.js';
import { generateOtp, parseTotpSettings } from './totp.js';

const byId = id => document.getElementById(id);
const form = byId('unlock-form');
const master = byId('master');
const vaultFile = byId('vault-file');
const keyFile = byId('key-file');
const drop = byId('drop');
const search = byId('search');
const groups = byId('groups');
const entries = byId('entries');
const detail = byId('detail');
const copyStatus = byId('copy-status');
const searchFields = ['Title', 'UserName', 'URL', 'Notes'];
const standardFields = new Set([...searchFields, 'Password']);
const otpFields = new Set(['otp', 'TimeOtp-Secret-Base32', 'TimeOtp-Length', 'TimeOtp-Period', 'TimeOtp-Algorithm']);
let file = null;
let database = null;
let workspace = null;
let group = null;
let selected = null;
let unlocking = false;
const copyTimers = new Map();
let copyStatusTimer;
let copyAttempt = 0;
let stopOtp;
let views = [];
let activeView = 0;
let paletteReturnFocus = null;
let fieldJumpReturnFocus = null;
let largeReturnFocus = null;
let zoomed = false;
let searchOrigin = null;

const panes = [groups.closest('.pane'), entries.closest('.pane'), detail];
const sidebarTriggers = [...document.querySelectorAll('[data-slot="sidebar-trigger"]')];

function setSidebar(trigger, open) {
  const panel = byId(trigger.getAttribute('aria-controls'));
  trigger.setAttribute('aria-expanded', String(open));
  panel.hidden = !open;
}

for (const trigger of sidebarTriggers) {
  trigger.addEventListener('click', () => {
    const open = trigger.getAttribute('aria-expanded') !== 'true';
    if (!open && trigger.getAttribute('aria-controls') === 'detail') hideReveals();
    setSidebar(trigger, open);
  });
}

function saveView(captureScroll = false) {
  const view = views[activeView];
  if (!view) return;
  Object.assign(view, { group, selected, query: search.value, zoomed });
  if (captureScroll) view.scroll = panes.map(pane => pane.scrollTop);
}

function updateTabLabel(tab, index, entry) {
  if (!tab) return;
  const title = entry ? field(entry, 'Title') || 'Untitled entry' : '';
  tab.textContent = title ? `${index + 1} ${title}` : String(index + 1);
  tab.setAttribute('aria-label', title ? `Tab ${index + 1}: ${title}` : `Tab ${index + 1}`);
}

function updateActiveTab() {
  saveView();
  updateTabLabel(byId('tabs').querySelector('[aria-selected="true"]'), activeView, selected);
}

function syncViews() {
  const tabs = byId('tabs');
  const wasTabsFocused = tabs.contains(document.activeElement);
  tabs.replaceChildren();
  views.forEach((view, index) => {
    const tab = button(undefined, () => switchView(index));
    tab.dataset.slot = 'tabs-trigger';
    tab.setAttribute('role', 'tab');
    tab.setAttribute('aria-selected', String(index === activeView));
    updateTabLabel(tab, index, view.selected);
    tabs.append(tab);
  });
  const add = button('+', newView);
  add.setAttribute('aria-label', 'New Tab');
  add.disabled = views.length >= 9;
  tabs.append(add);
  if (wasTabsFocused) tabs.querySelector('[aria-selected="true"]')?.focus();
}

function switchView(index, saveCurrent = true) {
  if (!views.length) return;
  if (saveCurrent) saveView(true);
  activeView = (index + views.length) % views.length;
  const view = views[activeView];
  ({ group, selected, zoomed } = view);
  search.value = view.query;
  byId('viewer').classList.toggle('zoomed', zoomed);
  renderGroups();
  renderEntries();
  syncViews();
  panes.forEach((pane, i) => { pane.scrollTop = view.scroll?.[i] ?? 0; });
}

function newView() {
  if (views.length >= 9) { copyStatus.textContent = 'Maximum of nine views.'; return; }
  saveView(true);
  views.push({ ...views[activeView] });
  switchView(views.length - 1, false);
}

function closeView() {
  if (views.length < 2) { lockVault(); return; }
  views.splice(activeView, 1);
  switchView(Math.min(activeView, views.length - 1), false);
}

function element(tag, text, className) {
  const node = document.createElement(tag);
  if (text !== undefined) node.textContent = text;
  if (className) node.className = className;
  return node;
}

function button(text, action) {
  const node = element('button', text);
  node.type = 'button';
  node.dataset.slot = 'button';
  node.addEventListener('click', action);
  return node;
}

function field(entry, key) {
  const value = entry.fields.get(key);
  return value instanceof ProtectedValue ? value.getText() : value ?? '';
}

function showMaster(visible) {
  const toggle = byId('show-master');
  master.type = visible ? 'text' : 'password';
  toggle.textContent = visible ? 'Hide' : 'Show';
  toggle.setAttribute('aria-label', visible ? 'Hide master password' : 'Show master password');
  toggle.setAttribute('aria-pressed', String(visible));
}

function chooseFile(candidate) {
  file = null;
  byId('file-hint').textContent = 'Choose or drop a .kdbx file';
  if (candidate && !/\.kdbx$/i.test(candidate.name)) {
    vaultFile.value = '';
    byId('error').textContent = 'Choose a KeePass .kdbx file.';
    return;
  }
  file = candidate || null;
  byId('error').textContent = '';
  if (file) byId('file-hint').textContent = file.name;
}

vaultFile.addEventListener('change', () => {
  const candidate = vaultFile.files[0];
  if (!database) {
    chooseFile(candidate);
  } else if (candidate && /\.kdbx$/i.test(candidate.name)) {
    lockVault(candidate);
    master.focus();
  } else {
    vaultFile.value = '';
  }
});
byId('show-master').addEventListener('click', () => showMaster(master.type === 'password'));
document.addEventListener('dragover', event => {
  if (event.dataTransfer.types.includes('Files')) event.preventDefault();
});
document.addEventListener('drop', event => {
  if (event.dataTransfer.types.includes('Files')) event.preventDefault();
});
drop.addEventListener('dragover', event => {
  if (!unlocking && event.dataTransfer.types.includes('Files')) drop.classList.add('active');
});
drop.addEventListener('dragleave', () => drop.classList.remove('active'));
drop.addEventListener('drop', event => {
  drop.classList.remove('active');
  const dropped = event.dataTransfer.files[0];
  if (unlocking || !dropped) return;
  vaultFile.value = '';
  chooseFile(dropped);
});

function unlockError(error) {
  if (error instanceof UnsupportedArgon2) return 'This vault uses an Argon2 version or variant the viewer cannot open.';
  switch (error.code) {
    case 'InvalidKey':
      return 'The vault could not be unlocked. Check the password and keyfile.';
    case 'FileCorrupt':
    case 'BadSignature':
    case 'InvalidArg':
      return 'The vault or keyfile is damaged or is not a valid KeePass file.';
    case 'InvalidVersion':
    case 'Unsupported':
    case 'NotImplemented':
      return 'This vault uses a KeePass format or encryption setting the viewer cannot open.';
    default:
      return 'The vault could not be read or decrypted. Check the files and try again.';
  }
}

function resetCredentials() {
  form.reset();
  showMaster(false);
  chooseFile(null);
  byId('progress').textContent = '';
}

form.addEventListener('submit', async event => {
  event.preventDefault();
  if (unlocking) return;
  if (!file) {
    byId('error').textContent = 'Choose or drop a .kdbx file first.';
    vaultFile.focus();
    return;
  }
  unlocking = true;
  byId('credentials').disabled = true;
  byId('error').textContent = '';
  byId('progress').textContent = 'Decrypting vault…';
  const key = keyFile.files[0];
  const name = file.name;
  const password = master.value === '' && key ? null : ProtectedValue.fromString(master.value);
  master.value = '';
  showMaster(false);
  let keyBytes;
  let credentials;
  try {
    const data = await file.arrayBuffer();
    keyBytes = key ? await key.arrayBuffer() : undefined;
    credentials = new Credentials(password, keyBytes);
    database = await Kdbx.load(data, credentials);
    workspace = vaultWorkspace(database);
    for (const trigger of sidebarTriggers) setSidebar(trigger, true);
    views = [{ group: null, selected: null, query: '', zoomed: false }]; activeView = 0;
    byId('vault-name').textContent = database.meta.name || name;
    renderGroups();
    renderEntries();
    syncViews();
    resetCredentials();
    byId('unlock-screen').hidden = true;
    byId('viewer').hidden = false;
    (entries.querySelector('[aria-current]') ?? search).focus();
  } catch (error) {
    clearVault();
    byId('error').textContent = unlockError(error);
  } finally {
    if (keyBytes) new Uint8Array(keyBytes).fill(0);
    if (credentials) {
      credentials.setPassword(null);
      credentials.setKeyFile(null);
    }
    unlocking = false;
    byId('credentials').disabled = false;
    byId('progress').textContent = '';
    if (!database) master.focus();
  }
});

function renderGroups() {
  groups.replaceChildren();
  for (const [label, items] of [['Views', workspace.builtIn], ['Tags', workspace.tags], ['Groups', workspace.groups]]) {
    if (!items.length) continue;
    const section = element('section');
    const list = element('ul');
    section.append(element('h2', label), list);
    for (const item of items) {
      const row = element('li');
      const node = button(undefined, () => {
        group = item === workspace.builtIn[0] ? null : item;
        search.value = '';
        selected = null;
        const previous = groups.querySelector('[aria-current]');
        previous?.removeAttribute('aria-current');
        if (previous) previous.tabIndex = -1;
        node.setAttribute('aria-current', 'true');
        node.tabIndex = 0;
        renderEntries();
        panes[1].scrollTop = 0;
      });
      if (item === (group ?? workspace.builtIn[0])) node.setAttribute('aria-current', 'true');
      node.tabIndex = node.hasAttribute('aria-current') ? 0 : -1;
      node.style.paddingInlineStart = `${0.55 + (item.depth ?? 0) * 0.8}rem`;
      node.append(element('span', item.name), element('small', String(item.rows.length), 'count'));
      row.append(node);
      list.append(row);
    }
    groups.append(section);
  }
}

function website(url) {
  try {
    const target = new URL(url);
    if (target.protocol === 'http:' || target.protocol === 'https:') return target.host;
  } catch { /* Display the stored text when it is not an absolute URL. */ }
  return url;
}

function searchAllEntries() {
  rememberSearch();
  group = null;
  renderGroups();
  renderEntries();
  search.focus();
}

function rememberSearch() {
  searchOrigin ??= { group, selected, query: search.value };
}

function cancelSearch() {
  if (searchOrigin) {
    ({ group, selected } = searchOrigin);
    search.value = searchOrigin.query;
    searchOrigin = null;
    renderGroups();
    renderEntries();
  }
  entries.querySelector('[aria-current]')?.focus();
  if (document.activeElement === search) search.blur();
}

function renderEntries() {
  const query = search.value.trim().toLowerCase();
  const matches = workspace.rows(group).filter(({ entry, searchable }) =>
    !query || ((searchable || group?.bin) && searchFields.some(key => field(entry, key).toLowerCase().includes(query)))
  );
  if (!matches.some(({ entry }) => entry === selected)) selected = matches[0]?.entry ?? null;
  const fragment = document.createDocumentFragment();
  for (const { entry } of matches) {
    const row = element('tr');
    row.dataset.slot = 'table-row';
    const node = button(undefined, () => {
      selected = entry;
      updateActiveTab();
      const previous = entries.querySelector('[aria-current]');
      previous?.removeAttribute('aria-current');
      if (previous) previous.tabIndex = -1;
      node.setAttribute('aria-current', 'true');
      node.tabIndex = 0;
      renderDetails();
    });
    node.textContent = field(entry, 'Title') || 'Untitled entry';
    if (selected === entry) node.setAttribute('aria-current', 'true');
    node.tabIndex = selected === entry ? 0 : -1;
    const title = element('td');
    title.append(node);
    const username = element('td', field(entry, 'UserName'));
    const url = element('td', website(field(entry, 'URL')));
    for (const cell of [title, username, url]) cell.dataset.slot = 'table-cell';
    row.append(title, username, url);
    row.addEventListener('click', event => {
      if (node.contains(event.target)) return;
      node.focus({ preventScroll: true });
      node.click();
    });
    fragment.append(row);
  }
  entries.replaceChildren(fragment);
  byId('result-count').textContent = matches.length === 1 ? '1 entry' : `${matches.length} entries`;
  renderDetails();
  updateActiveTab();
}

function copyFallback(value) {
  const previousFocus = document.activeElement;
  const node = element('textarea');
  node.value = value;
  node.readOnly = true;
  node.style.cssText = 'position:fixed;top:0;left:0;opacity:0';
  document.body.append(node);
  try {
    node.select();
    if (!document.execCommand('copy')) throw new Error('Clipboard unavailable');
  } finally {
    node.value = '';
    node.remove();
    previousFocus?.focus();
  }
}

function resetCopyStatus() {
  copyAttempt++;
  clearTimeout(copyStatusTimer);
  for (const timer of copyTimers.values()) clearTimeout(timer);
  copyTimers.clear();
  copyStatus.textContent = '';
}

function copyButton(readValue, label, caption = 'Copy') {
  const node = button(caption, async () => {
    const value = readValue();
    if (value == null) return;
    const attempt = ++copyAttempt;
    let message = 'Copied!';
    try {
      if (navigator.clipboard) {
        try {
          await navigator.clipboard.writeText(value);
        } catch {
          if (attempt !== copyAttempt) return;
          copyFallback(value);
        }
      } else {
        copyFallback(value);
      }
    } catch {
      message = 'Clipboard unavailable';
    }
    if (attempt !== copyAttempt) return;
    if (message === 'Copied!') workspace?.recordUse(selected);
    node.textContent = message;
    copyStatus.textContent = message;
    clearTimeout(copyTimers.get(node));
    copyTimers.set(node, setTimeout(() => {
      node.textContent = caption;
      copyTimers.delete(node);
    }, 1500));
    clearTimeout(copyStatusTimer);
    copyStatusTimer = setTimeout(() => { copyStatus.textContent = ''; }, 1500);
  });
  node.setAttribute('aria-label', 'Copy ' + label);
  return node;
}

function otpDisplay(entry) {
  const row = element('div', undefined, 'otp');
  let settings;
  try {
    settings = parseTotpSettings({
      uri: field(entry, 'otp'),
      secret: field(entry, 'TimeOtp-Secret-Base32'),
      digits: field(entry, 'TimeOtp-Length'),
      period: field(entry, 'TimeOtp-Period'),
      algorithm: field(entry, 'TimeOtp-Algorithm')
    });
  } catch {
    row.textContent = 'Invalid TOTP settings.';
    return row;
  }
  const { secret, digits, period, hash } = settings;
  const code = element('span', '—', 'otp-code');
  code.dataset.fieldKey = 'otp';
  const countdown = element('small');
  const progress = element('progress');
  progress.dataset.slot = 'progress';
  progress.max = period;
  progress.setAttribute('aria-label', 'Time remaining for this code');
  const stepMs = period * 1000;
  let active = true;
  let key;
  let timer;
  let requestedStep;
  let displayedStep;
  const currentStep = () => Math.floor(Date.now() / stepMs);
  const copy = copyButton(() => displayedStep === currentStep() ? code.textContent : null, 'OTP', 'Copy OTP');
  copy.dataset.fieldKey = 'otp';
  copy.disabled = true;
  const actions = element('div', undefined, 'inline');
  actions.append(code, copy);
  row.append(actions, countdown, progress);

  stopOtp = () => {
    active = false;
    clearInterval(timer);
    key = undefined;
    code.textContent = '—';
  };

  async function tick() {
    const now = Date.now();
    const step = Math.floor(now / stepMs);
    const remaining = (stepMs - now % stepMs) / 1000;
    countdown.textContent = Math.ceil(remaining) + 's remaining';
    progress.value = remaining;
    if (step === requestedStep) return;
    requestedStep = step;
    displayedStep = undefined;
    code.textContent = '—';
    copy.disabled = true;
    try {
      const value = await generateOtp(key, step, digits);
      if (!active || requestedStep !== step || currentStep() !== step) return;
      displayedStep = step;
      code.textContent = value;
      copy.disabled = false;
    } catch {
      if (active) showUnavailable();
    }
  }

  function showUnavailable() {
    stopOtp();
    row.textContent = 'Could not generate the TOTP code.';
  }

  async function start() {
    try {
      const imported = await crypto.subtle.importKey('raw', secret, { name: 'HMAC', hash }, false, ['sign']);
      if (!active) return;
      key = imported;
      timer = setInterval(tick, 200);
      tick();
    } catch {
      if (active) showUnavailable();
    } finally {
      secret.fill(0);
    }
  }
  start();
  return row;
}

function clearOtp() {
  stopOtp?.();
  stopOtp = undefined;
}

function renderDetails() {
  clearOtp();
  resetCopyStatus();
  detail.replaceChildren(element('h2', 'Entry details'));
  const entry = selected;
  if (!entry) {
    detail.append(element('p', 'Select an entry to view its details.', 'muted'));
    return;
  }
  const summary = element('div', undefined, 'detail-summary');
  const title = field(entry, 'Title') || 'Untitled entry';
  const tags = element('div', undefined, 'tags');
  for (const tag of entry.tags) {
    const badge = element('span', tag);
    badge.dataset.slot = 'badge';
    tags.append(badge);
  }
  const heading = element('div', undefined, 'detail-heading');
  heading.append(element('h2', title), tags);
  summary.append(heading);
  const row = workspace.row(entry);
  if (row) summary.append(element('p', row.path.join(' / '), 'muted'));
  if (Number.isFinite(entry.lastModTime) && entry.times.lastModTime) {
    summary.append(element('p', 'Modified ' + entry.times.lastModTime.toISOString().slice(0, 10), 'muted'));
  }
  detail.replaceChildren(summary);
  const list = element('dl');
  function add(label, value, ...actions) {
    const contents = element('dd', undefined, actions.length ? 'inline' : undefined);
    contents.dataset.fieldLabel = label;
    const fieldKey = label === 'Username' ? 'UserName' : label === 'One-time password (TOTP)' ? 'otp' : label;
    contents.dataset.fieldKey = fieldKey; contents.tabIndex = 0;
    value.dataset.fieldValueKey = fieldKey;
    contents.append(value, ...actions);
    list.append(element('dt', label), contents);
  }
  function masked(key, label, copy = false) {
    if (!entry.fields.get(key)) {
      add(label, element('span', '—'));
      return;
    }
    const value = element('span', '••••••••', 'value');
    value.dataset.fieldKey = key;
    const toggle = button('Reveal', () => {
      const visible = toggle.getAttribute('aria-pressed') === 'true';
      value.textContent = visible ? '••••••••' : field(entry, key);
      if (!visible) workspace.recordUse(entry);
      toggle.textContent = visible ? 'Reveal' : 'Hide';
      toggle.setAttribute('aria-pressed', String(!visible));
    });
    toggle.setAttribute('aria-pressed', 'false');
    toggle.setAttribute('aria-label', 'Reveal or hide ' + label);
    toggle.dataset.fieldKey = key; toggle.dataset.fieldAction = 'reveal';
    add(label, value, toggle, ...(copy ? [copyButton(() => field(entry, key), label)] : []));
  }
  add('Title', element('span', field(entry, 'Title') || 'Untitled entry'));
  const username = field(entry, 'UserName');
  const usernameNode = element('span', username || '—', 'value'); usernameNode.dataset.fieldKey = 'UserName';
  add('Username', usernameNode, ...(username ? [copyButton(() => field(entry, 'UserName'), 'username')] : []));
  masked('Password', 'Password', true);
  if (entry.fields.has('otp') || entry.fields.has('TimeOtp-Secret-Base32')) {
    add('One-time password (TOTP)', otpDisplay(entry));
  }
  const url = field(entry, 'URL');
  let urlNode = element('span', url || '—');
  try {
    const target = new URL(url);
    if (target.protocol === 'https:' || target.protocol === 'http:') {
      urlNode = element('a', url);
      urlNode.href = target.href;
      urlNode.target = '_blank';
      urlNode.rel = 'noopener noreferrer';
    }
  } catch {
    // Display KeePass placeholders and other non-URL values as text.
  }
  urlNode.dataset.fieldKey = 'URL'; add('URL', urlNode, ...(url ? [copyButton(() => field(entry, 'URL'), 'URL')] : []));
  const notes = field(entry, 'Notes'); const notesNode = element('span', notes || '—'); notesNode.dataset.fieldKey = 'Notes'; add('Notes', notesNode, ...(notes ? [copyButton(() => field(entry, 'Notes'), 'Notes')] : []));
  for (const [key, value] of entry.fields) {
    if (standardFields.has(key) || otpFields.has(key)) continue;
    if (value instanceof ProtectedValue) masked(key, key, true);
    else { const custom = element('span', value || '—'); custom.dataset.fieldKey = key; add(key, custom, ...(value ? [copyButton(() => field(entry, key), key)] : [])); }
  }
  detail.append(list);
}

function clearVault() {
  clearOtp();
  database = null;
  workspace = null;
  searchOrigin = null;
  group = null;
  selected = null;
  resetCopyStatus();
  search.value = '';
  groups.replaceChildren();
  entries.replaceChildren();
  detail.replaceChildren();
  byId('vault-name').textContent = '';
  byId('result-count').textContent = '';
  byId('large-content').textContent = ''; byId('palette-search').value = ''; byId('palette-items').replaceChildren();
  byId('tabs').replaceChildren(); byId('viewer').classList.remove('zoomed');
}

search.addEventListener('focus', rememberSearch);
search.addEventListener('blur', () => {
  if (byId('palette').hidden) searchOrigin = null;
});
search.addEventListener('input', () => {
  if (database) renderEntries();
});
function lockVault(preserveFile = null) {
  closeOverlays();
  views = [];
  activeView = 0;
  zoomed = false;
  clearVault();
  resetCredentials();
  if (preserveFile) chooseFile(preserveFile);
  byId('viewer').hidden = true;
  byId('unlock-screen').hidden = false;
  vaultFile.focus();
}
byId('lock').addEventListener('click', () => lockVault());

function openPalette() {
  if (!byId('large-view').hidden) closeOverlays();
  paletteReturnFocus = document.activeElement;
  hideReveals();
  const box = byId('palette'); box.hidden = false;
  renderPalette(''); byId('palette-search').value = ''; byId('palette-search').focus();
}
function openVault() {
  vaultFile.click();
}

function closeOverlays() {
  const active = document.activeElement;
  const returnFocus = active?.closest('#palette') ? paletteReturnFocus
    : active?.closest('#field-jump') ? fieldJumpReturnFocus
    : active?.closest('#large-view') ? largeReturnFocus : null;
  for (const id of ['palette', 'field-jump', 'large-view']) byId(id).hidden = true;
  byId('large-title').textContent = 'Large View';
  byId('large-content').textContent = '';
  paletteReturnFocus = null;
  fieldJumpReturnFocus = null;
  largeReturnFocus = null;
  returnFocus?.focus();
}
function commandDefs() {
  const needsEntry = selected ? undefined : 'Select an entry first.';
  return [
    ['Open vault', 'o', openVault], ['Jump to field', 'Shift+J', openFieldJump, needsEntry],
    ['Lock Vault', 'Shift+L', lockVault], ['Close Tab', '', closeView],
    ['New Tab', 't', newView], ['Previous Tab', '[', () => switchView(activeView - 1)], ['Next Tab', ']', () => switchView(activeView + 1)],
    ['Search', '/', searchAllEntries], ['Toggle dark mode', '', () => byId('theme').click()], ['Copy focused field', 'c', copyFocusedField, needsEntry], ['Reveal focused field', 'v', revealFocusedField, needsEntry],
    ['Large View', 'Shift+G', openLargeView, needsEntry], ['Toggle zoom', 'z', toggleZoom]
  ];
}
function renderPalette(query) {
  const host = byId('palette-items');
  const filter = query.trim().toLowerCase();
  const filtered = commandDefs().filter(([name, keys]) => `${name} ${keys}`.toLowerCase().includes(filter));
  host.replaceChildren(...filtered.map(([name, keys, run, disabledReason]) => {
    const row = button(name, () => {
      if (disabledReason) return;
      closeOverlays();
      run();
    });
    row.className = 'command';
    row.dataset.slot = 'command-item';
    if (keys) row.append(element('small', keys));
    if (disabledReason) {
      row.setAttribute('aria-disabled', 'true');
      row.append(element('small', disabledReason));
    }
    return row;
  }));
  host.querySelector('button')?.setAttribute('aria-current', 'true');
}
byId('commands').addEventListener('click', openPalette);
byId('palette-close').addEventListener('click', closeOverlays);
byId('palette-search').addEventListener('input', event => renderPalette(event.target.value));
function activeField() { return document.activeElement?.closest('[data-field-key]')?.dataset.fieldKey; }
function hideReveals() {
  const focused = document.activeElement;
  for (const toggle of detail.querySelectorAll('[data-field-action="reveal"][aria-pressed="true"]')) toggle.click();
  focused?.focus?.();
}
function fieldNode(key) { return [...detail.querySelectorAll('[data-field-key]')].find(node => node.dataset.fieldKey.toLowerCase() === key.toLowerCase()); }
function fieldValueNode(key) { return [...detail.querySelectorAll('[data-field-value-key]')].find(node => node.dataset.fieldValueKey.toLowerCase() === key.toLowerCase()); }
function focusField(key) {
  const node = fieldNode(key);
  if (!node) return;
  if (detail.hidden) setSidebar(sidebarTriggers.find(trigger => trigger.getAttribute('aria-controls') === detail.id), true);
  node.tabIndex = 0;
  node.focus();
  node.scrollIntoView({ block: 'nearest' });
}
function actionNode(key, action) { return detail.querySelector(`[data-field-action="${action}"][data-field-key="${CSS.escape(key)}"]`); }
function copyFocusedField() {
  const key = activeField() || (selected && selected.fields.has('Password') ? 'Password' : selected && selected.fields.has('UserName') ? 'UserName' : null);
  if (!key) return;
  const node = [...detail.querySelectorAll('button[aria-label^="Copy "]')].find(item => item.getAttribute('aria-label').toLowerCase() === `copy ${key === 'UserName' ? 'username' : key}`.toLowerCase());
  node?.click();
}
function revealFocusedField() {
  const key = activeField() || (selected?.fields.has('Password') ? 'Password' : null);
  const node = key && [...detail.querySelectorAll('button[aria-label^="Reveal or hide "]')].find(item => item.dataset.fieldKey === key);
  node?.click();
}
function openLargeView() {
  const key = activeField() || (selected?.fields.has('Password') ? 'Password' : selected?.fields.has('UserName') ? 'UserName' : null);
  if (!key) return;
  workspace.recordUse(selected);
  hideReveals();
  const text = actionNode(key, 'reveal') ? field(selected, key) : fieldValueNode(key)?.textContent;
  byId('large-title').textContent = key;
  byId('large-content').textContent = text || '—';
  largeReturnFocus = document.activeElement;
  byId('large-view').hidden = false;
  byId('large-close').focus();
}
byId('large-close').addEventListener('click', closeOverlays);
function toggleZoom() { zoomed = !zoomed; byId('viewer').classList.toggle('zoomed', zoomed); saveView(); }
function openFieldJump() {
  if (!selected) return;
  fieldJumpReturnFocus = document.activeElement;
  hideReveals();
  byId('field-jump').hidden = false;
  byId('field-jump').focus();
}

function jumpField(letter) {
  const names = { p: 'Password', u: 'UserName', w: 'URL', n: 'Notes', e: 'Email' };
  closeOverlays();
  focusField(names[letter]);
}

function handlePaletteKey(event) {
  const input = byId('palette-search');
  const close = byId('palette-close');
  const options = [...byId('palette-items').querySelectorAll('button')];
  const index = options.findIndex(option => option.hasAttribute('aria-current'));
  const { key, shiftKey, ctrlKey, metaKey, altKey } = event;

  if (key === 'Escape') {
    event.preventDefault();
    closeOverlays();
  } else if (key === 'Enter' && event.target !== close) {
    event.preventDefault();
    options[index]?.click();
  } else if (key === 'Tab') {
    event.preventDefault();
    const controls = [close, input, ...options];
    const direction = shiftKey ? -1 : 1;
    const current = controls.indexOf(event.target);
    const next = controls[(current + direction + controls.length) % controls.length];
    if (options.includes(next)) {
      options[index]?.removeAttribute('aria-current');
      next.setAttribute('aria-current', 'true');
    }
    next.focus();
  } else if (key === 'ArrowDown' || key === 'ArrowUp') {
    event.preventDefault();
    if (!options.length) return;
    const direction = key === 'ArrowUp' ? -1 : 1;
    const next = options[(index + direction + options.length) % options.length];
    options[index]?.removeAttribute('aria-current');
    next.setAttribute('aria-current', 'true');
    next.focus();
  } else if (options.includes(event.target) && !ctrlKey && !metaKey && !altKey && (key.length === 1 || key === 'Backspace')) {
    event.preventDefault();
    input.value = key === 'Backspace' ? input.value.slice(0, -1) : input.value + key;
    renderPalette(input.value);
    input.focus();
  }
}

function moveWithinPane(event) {
  const { key, target } = event;
  if (key !== 'ArrowDown' && key !== 'ArrowUp') return false;
  const list = target.closest('#groups, #entries');
  const row = target.closest('#detail dd');
  const items = list
    ? [...list.querySelectorAll('button')]
    : row ? [...detail.querySelectorAll('dd')] : [];
  const index = items.indexOf(row ?? target);
  if (index < 0) return false;
  event.preventDefault();
  const next = index + (key === 'ArrowDown' ? 1 : -1);
  const item = items[next];
  item?.focus();
  if (list) item?.click();
  return true;
}

document.addEventListener('keydown', event => {
  const target = event.target;
  const editing = target.matches('input, textarea, select, [contenteditable="true"]');
  if (!byId('palette').hidden) {
    handlePaletteKey(event);
    return;
  }
  if (!byId('field-jump').hidden) {
    if (event.key === 'Tab') { event.preventDefault(); return; }
    if (event.key === 'Escape') { event.preventDefault(); closeOverlays(); }
    else if (/^[puwne]$/i.test(event.key)) { event.preventDefault(); jumpField(event.key.toLowerCase()); }
    return;
  }
  if (!byId('large-view').hidden) {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'p') { event.preventDefault(); closeOverlays(); openPalette(); }
    else if (event.shiftKey && event.key.toLowerCase() === 'l') { event.preventDefault(); lockVault(); }
    else if (event.key === 'Tab') { event.preventDefault(); byId('large-close').focus(); }
    else if (event.key === 'Escape' || event.key === 'Enter') { event.preventDefault(); closeOverlays(); }
    return;
  }
  if (target === search && event.key === 'Escape') { event.preventDefault(); cancelSearch(); return; }
  if (event.key === 'Escape') { closeOverlays(); return; }
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'p' && database) { event.preventDefault(); openPalette(); return; }
  if (database && target === search && event.key === 'ArrowDown') { event.preventDefault(); (entries.querySelector('[aria-current]') || entries.querySelector('button'))?.focus(); return; }
  if (database && target === search && event.key === 'Enter') { const first = entries.querySelector('[aria-current]') || entries.querySelector('button'); if (first) { event.preventDefault(); first.click(); first.focus(); return; } }
  if (editing || !database) return;
  if (event.ctrlKey || event.metaKey || event.altKey) return;
  if (moveWithinPane(event)) return;
  if (event.key === '?' && !event.ctrlKey && !event.metaKey) { event.preventDefault(); openPalette(); }
  else if (event.key === '/') { event.preventDefault(); search.focus(); }
  else if (event.shiftKey && event.key.toLowerCase() === 'j') { event.preventDefault(); openFieldJump(); }
  else if (event.shiftKey && event.key.toLowerCase() === 'l') { event.preventDefault(); lockVault(); }
  else if (event.shiftKey && event.key.toLowerCase() === 'g') { event.preventDefault(); openLargeView(); }
  else if (event.key.toLowerCase() === 'o') openVault();
  else if (event.key.toLowerCase() === 'c') copyFocusedField();
  else if (event.key.toLowerCase() === 'v') revealFocusedField();
  else if (event.key.toLowerCase() === 'z') toggleZoom();
  else if (event.key.toLowerCase() === 't') newView();
  else if (event.key === '[') switchView(activeView - 1);
  else if (event.key === ']') switchView(activeView + 1);
  else if (/^[1-9]$/.test(event.key) && views[Number(event.key) - 1]) switchView(Number(event.key) - 1);
});
