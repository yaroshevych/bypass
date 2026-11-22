import { Credentials, Kdbx, ProtectedValue, UnsupportedArgon2 } from './crypto.js';
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
let zoomed = false;
let fieldJumpPending = false;

function saveView() { if (views[activeView]) Object.assign(views[activeView], { group, selected, query: search.value, zoomed }); }
function syncViews() {
  const tabs = byId('tabs'); const wasTabsFocused = tabs.contains(document.activeElement); tabs.replaceChildren();
  views.forEach((view, index) => {
    const tab = button(`Tab ${index + 1}`, () => switchView(index));
    tab.setAttribute('role', 'tab'); tab.setAttribute('aria-selected', String(index === activeView));
    tab.setAttribute('aria-label', `Tab ${index + 1}${view.selected ? ': ' + (field(view.selected, 'Title') || 'Untitled entry') : ''}`);
    tabs.append(tab);
  });
  if (wasTabsFocused) tabs.querySelector(`[role="tab"][aria-selected="true"]`)?.focus();
}
function switchView(index) {
  if (!views.length) return;
  saveView(); activeView = (index + views.length) % views.length;
  const view = views[activeView]; group = view.group; selected = view.selected; zoomed = view.zoomed;
  search.value = view.query; byId('viewer').classList.toggle('zoomed', zoomed);
  renderGroups(); renderEntries(); syncViews();
}
function newView() { saveView(); views.push({ group: null, selected: null, query: '', zoomed: false }); switchView(views.length - 1); }
function closeView() {
  if (views.length < 2) { lockVault(); return; }
  views.splice(activeView, 1); activeView = Math.min(activeView, views.length - 1); switchView(activeView);
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

vaultFile.addEventListener('change', () => chooseFile(vaultFile.files[0]));
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
    views = [{ group: null, selected: null, query: '', zoomed: false }]; activeView = 0;
    byId('vault-name').textContent = database.meta.name || name;
    renderGroups();
    renderEntries();
    resetCredentials();
    byId('unlock-screen').hidden = true;
    byId('viewer').hidden = false;
    search.focus();
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

function visibleGroup(candidate) {
  return candidate.uuid.id !== database.meta.recycleBinUuid?.id;
}

function renderGroups() {
  function groupButton(name, value) {
    const node = button(name, () => {
      group = value;
      search.value = '';
      selected = null;
      saveView();
      for (const item of groups.querySelectorAll('button[aria-current]')) item.removeAttribute('aria-current');
      node.setAttribute('aria-current', 'true');
      renderEntries();
    });
    if (group === value) node.setAttribute('aria-current', 'true');
    return node;
  }

  function groupRow(value) {
    const row = element('li');
    row.append(groupButton(value.name || 'Unnamed group', value));
    const children = value.groups.filter(visibleGroup);
    if (children.length) {
      const branch = element('details');
      branch.open = true;
      const list = element('ul');
      for (const child of children) list.append(groupRow(child));
      branch.append(element('summary', 'Subgroups of ' + (value.name || 'unnamed group')), list);
      row.append(branch);
    }
    return row;
  }

  const all = element('li');
  all.append(groupButton('All entries', null));
  groups.replaceChildren(all);
  for (const root of database.groups.filter(visibleGroup)) groups.append(groupRow(root));
  syncViews();
}

function* walkGroups(roots, parents = []) {
  for (const node of roots) {
    if (!visibleGroup(node)) continue;
    const path = [...parents, node.name || 'Unnamed group'];
    for (const entry of node.entries) yield { entry, path };
    yield* walkGroups(node.groups, path);
  }
}

function renderEntries() {
  const query = search.value.trim().toLowerCase();
  const rows = !query && group
    ? group.entries.map(entry => ({ entry, path: [group.name] }))
    : [...walkGroups(database.groups)];
  const matches = rows.filter(({ entry }) =>
    !query || searchFields.some(key => field(entry, key).toLowerCase().includes(query))
  );
  if (!matches.some(({ entry }) => entry === selected)) selected = null;
  const fragment = document.createDocumentFragment();
  for (const { entry, path } of matches) {
    const row = element('li');
    const node = button(undefined, () => {
      selected = entry;
      saveView(); syncViews();
      entries.querySelector('[aria-current]')?.removeAttribute('aria-current');
      node.setAttribute('aria-current', 'true');
      renderDetails();
    });
    node.append(element('span', field(entry, 'Title') || 'Untitled entry'));
    const subtitle = [field(entry, 'UserName'), path.join(' / ')].filter(Boolean).join(' · ');
    if (subtitle) node.append(element('small', subtitle));
    if (selected === entry) node.setAttribute('aria-current', 'true');
    row.append(node);
    fragment.append(row);
  }
  entries.replaceChildren(fragment);
  byId('result-count').textContent = matches.length === 1 ? '1 entry' : `${matches.length} entries`;
  renderDetails();
  syncViews();
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

function decodeBase32(secret) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  const normalized = secret.toUpperCase().replace(/\s/g, '');
  const [, body, padding] = /^([A-Z2-7]+)(=*)$/.exec(normalized) ?? [];
  if (!body || ![0, 2, 4, 5, 7].includes(body.length % 8)
      || (padding && padding.length !== (8 - body.length % 8) % 8)) {
    throw new Error('Invalid Base32 secret');
  }
  const bytes = new Uint8Array(Math.floor(body.length * 5 / 8));
  let bits = 0;
  let buffer = 0;
  let offset = 0;
  for (const character of body) {
    buffer = (buffer << 5) | alphabet.indexOf(character);
    bits += 5;
    if (bits >= 8) {
      bits -= 8;
      bytes[offset++] = buffer >> bits;
      buffer &= (1 << bits) - 1;
    }
  }
  if (buffer !== 0) {
    bytes.fill(0);
    throw new Error('Invalid Base32 padding bits');
  }
  return bytes;
}

function otpSettings(entry) {
  let secret = field(entry, 'TimeOtp-Secret-Base32');
  let digits = field(entry, 'TimeOtp-Length');
  let period = field(entry, 'TimeOtp-Period');
  let algorithm = field(entry, 'TimeOtp-Algorithm');
  const uri = field(entry, 'otp').trim();
  if (uri) {
    const url = new URL(uri);
    if (url.protocol !== 'otpauth:' || url.hostname.toLowerCase() !== 'totp') {
      throw new Error('Invalid TOTP URI');
    }
    secret = url.searchParams.get('secret') ?? '';
    digits = url.searchParams.get('digits') ?? '';
    period = url.searchParams.get('period') ?? '';
    algorithm = url.searchParams.get('algorithm') ?? '';
  }
  digits = digits.trim() || '6';
  period = period.trim() || '30';
  const hash = /^(?:HMAC-)?SHA-?(1|256|512)$/i.exec(algorithm.trim() || 'SHA1');
  if (!/^[6-8]$/.test(digits) || !/^\d+$/.test(period)
      || !Number.isSafeInteger(Number(period) * 1000) || Number(period) < 1 || !hash) {
    throw new Error('Invalid TOTP settings');
  }
  return { secret: decodeBase32(secret), digits: Number(digits), period: Number(period), hash: 'SHA-' + hash[1] };
}

async function generateOtp(key, counter, digits) {
  const message = new DataView(new ArrayBuffer(8));
  message.setBigUint64(0, BigInt(counter));
  const signature = new DataView(await crypto.subtle.sign('HMAC', key, message));
  const offset = signature.getUint8(signature.byteLength - 1) & 15;
  const value = signature.getUint32(offset) & 0x7fffffff;
  return String(value % 10 ** digits).padStart(digits, '0');
}

function otpDisplay(entry) {
  const row = element('div', undefined, 'otp');
  let settings;
  try {
    settings = otpSettings(entry);
  } catch {
    row.textContent = 'Invalid TOTP settings.';
    return row;
  }
  const { secret, digits, period, hash } = settings;
  const code = element('span', '—', 'otp-code');
  const countdown = element('small');
  const progress = element('progress');
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
  const list = element('dl');
  function add(label, value, ...actions) {
    const contents = element('dd', undefined, actions.length ? 'inline' : undefined);
    contents.dataset.fieldLabel = label;
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
    if (value instanceof ProtectedValue) masked(key, key);
    else { const custom = element('span', value || '—'); custom.dataset.fieldKey = key; add(key, custom, ...(value ? [copyButton(() => field(entry, key), key)] : [])); }
  }
  detail.append(list);
}

function clearVault() {
  clearOtp();
  database = null;
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
}

search.addEventListener('input', () => {
  if (database) { renderEntries(); saveView(); }
});
function lockVault() {
  closeOverlays(); views = []; activeView = 0; zoomed = false;
  clearVault();
  resetCredentials();
  byId('viewer').hidden = true;
  byId('unlock-screen').hidden = false;
  vaultFile.focus();
}
byId('lock').addEventListener('click', lockVault);

function openPalette() {
  paletteReturnFocus = document.activeElement;
  const box = byId('palette'); box.hidden = false;
  renderPalette(''); byId('palette-search').value = ''; byId('palette-search').focus();
}
function closeOverlays() {
  const active = document.activeElement;
  for (const id of ['palette', 'field-jump', 'large-view']) byId(id).hidden = true;
  fieldJumpPending = false;
  byId('large-content').textContent = '';
  if (active?.closest('#palette')) paletteReturnFocus?.focus();
  else if (active?.closest('#field-jump')) fieldJumpReturnFocus?.focus();
  else if (active?.closest('#large-view')) largeReturnFocus?.focus();
}
const commandDefs = [
  ['Lock vault', 'Shift+L', lockVault], ['Close current view', 'Ctrl+W', closeView],
  ['New view', 't', newView], ['Previous view', '[', () => switchView(activeView - 1)], ['Next view', ']', () => switchView(activeView + 1)],
  ['Focus search', '/', () => search.focus()], ['Copy focused field', 'c', copyFocusedField], ['Reveal focused field', 'v', revealFocusedField],
  ['Large View', 'Shift+G', openLargeView], ['Toggle zoom', 'z', toggleZoom]
];
function renderPalette(query) {
  const host = byId('palette-items'); const filtered = commandDefs.filter(([name, keys]) => `${name} ${keys}`.toLowerCase().includes(query.toLowerCase()));
  host.replaceChildren(...filtered.map(([name, keys, run]) => {
    const row = button(name, () => { byId('palette').hidden = true; run(); }); row.className = 'command'; row.append(element('small', keys)); return row;
  }));
  host.querySelector('button')?.setAttribute('aria-current', 'true');
}
byId('commands').addEventListener('click', openPalette);
byId('palette-search').addEventListener('input', event => renderPalette(event.target.value));
function activeField() { return document.activeElement?.closest('[data-field-key]')?.dataset.fieldKey; }
function fieldNode(key) { return [...detail.querySelectorAll('[data-field-key]')].find(node => node.dataset.fieldKey.toLowerCase() === key.toLowerCase()); }
function focusField(key) { const node = fieldNode(key); if (!node) return false; node.tabIndex = 0; node.focus(); node.scrollIntoView({ block: 'nearest' }); return true; }
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
let largeReturnFocus = null;
function openLargeView() {
  const key = activeField() || (selected?.fields.has('Password') ? 'Password' : selected?.fields.has('UserName') ? 'UserName' : null);
  if (!key) return;
  const node = fieldNode(key); const reveal = actionNode(key, 'reveal');
  const text = reveal?.getAttribute('aria-pressed') === 'true' ? node.textContent : (key === 'Password' ? '••••••••' : node?.textContent);
  byId('large-title').textContent = key; byId('large-content').textContent = text || '—';
  largeReturnFocus = document.activeElement; byId('large-view').hidden = false; byId('large-close').focus();
}
byId('large-close').addEventListener('click', closeOverlays);
function toggleZoom() { zoomed = !zoomed; byId('viewer').classList.toggle('zoomed', zoomed); saveView(); }
function openFieldJump() { fieldJumpReturnFocus = document.activeElement; byId('field-jump').hidden = false; fieldJumpPending = true; }
function jumpField(letter) { const names = { p: 'Password', u: 'UserName', w: 'URL', n: 'Notes', e: 'Email' }; byId('field-jump').hidden = true; fieldJumpPending = false; focusField(names[letter]); }

document.addEventListener('keydown', event => {
  const target = event.target;
  const editing = target.matches('input, textarea, select, [contenteditable="true"]');
  const palette = !byId('palette').hidden;
  if (palette) {
    const options = [...byId('palette-items').querySelectorAll('button')]; let index = options.findIndex(item => item.getAttribute('aria-current') === 'true');
    if (event.key === 'Escape') { event.preventDefault(); closeOverlays(); }
    else if (event.key === 'ArrowDown' || event.key === 'Tab' && !event.shiftKey) { event.preventDefault(); if (options.length) { index = (index + 1) % options.length; options.forEach((item, i) => item.setAttribute('aria-current', String(i === index))); options[index].focus(); } }
    else if (event.key === 'ArrowUp' || event.key === 'Tab' && event.shiftKey) { event.preventDefault(); if (options.length) { index = (index <= 0 ? options.length : index) - 1; options.forEach((item, i) => item.setAttribute('aria-current', String(i === index))); options[index].focus(); } }
    else if (event.key === 'Enter') { event.preventDefault(); options[index]?.click(); }
    return;
  }
  if (fieldJumpPending) {
    if (event.key === 'Escape') { event.preventDefault(); closeOverlays(); }
    else if (/^[puwne]$/i.test(event.key)) { event.preventDefault(); jumpField(event.key.toLowerCase()); }
    return;
  }
  if (!byId('large-view').hidden) { if (event.key === 'Escape' || event.key === 'Enter') { event.preventDefault(); closeOverlays(); } return; }
  if (target === search && event.key === 'Escape') { event.preventDefault(); search.blur(); return; }
  if (event.key === 'Escape') { closeOverlays(); return; }
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'p' && database) { event.preventDefault(); openPalette(); return; }
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'w' && database) { event.preventDefault(); closeView(); return; }
  if (database && target === search && event.key === 'ArrowDown') { event.preventDefault(); (entries.querySelector('[aria-current]') || entries.querySelector('button'))?.focus(); return; }
  if (database && target === search && event.key === 'Enter') { const first = entries.querySelector('[aria-current]') || entries.querySelector('button'); if (first) { event.preventDefault(); first.click(); first.focus(); return; } }
  if (editing || !database) return;
  if (event.ctrlKey || event.metaKey || event.altKey) return;
  const list = target.closest('#groups, #entries');
  if (list && ['ArrowDown', 'ArrowUp'].includes(event.key)) {
    const options = [...list.querySelectorAll('button')]; const current = options.indexOf(target);
    if (current >= 0) { event.preventDefault(); options[Math.max(0, Math.min(options.length - 1, current + (event.key === 'ArrowDown' ? 1 : -1)))]?.focus(); return; }
  }
  if (event.key === '?' && !event.ctrlKey && !event.metaKey) { event.preventDefault(); openPalette(); }
  else if (event.key === '/') { event.preventDefault(); search.focus(); }
  else if (event.shiftKey && event.key.toLowerCase() === 'j') { event.preventDefault(); openFieldJump(); }
  else if (event.shiftKey && event.key.toLowerCase() === 'l') { event.preventDefault(); lockVault(); }
  else if (event.shiftKey && event.key.toLowerCase() === 'g') { event.preventDefault(); openLargeView(); }
  else if (event.key.toLowerCase() === 'c') copyFocusedField();
  else if (event.key.toLowerCase() === 'v') revealFocusedField();
  else if (event.key.toLowerCase() === 'z') toggleZoom();
  else if (event.key.toLowerCase() === 't') newView();
  else if (event.key === '[') switchView(activeView - 1);
  else if (event.key === ']') switchView(activeView + 1);
  else if (/^[1-9]$/.test(event.key) && views[Number(event.key) - 1]) switchView(Number(event.key) - 1);
});
