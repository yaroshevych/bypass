import { installDialogDismissal } from './dialogs.js';
import { setActionIcon } from './action-icons.js';
import './themes.js';
import { vaultWorkspace } from './workspace.js';
import { Credentials, Kdbx, ProtectedValue, UnsupportedArgon2 } from './crypto.js';
import { generateOtp, parseTotpSettings } from './totp.js';

const byId = id => document.getElementById(id);
const form = byId('unlock-form');
const master = byId('master');
const vaultFile = byId('vault-file');
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
let vaultFileInfo = null;
let group = null;
let selected = null;
let unlocking = false;
let readOnly = true;
let dirty = false;
let revision = 0;
let saving = false;
const copyTimers = new Map();
let copyStatusTimer;
let copyAttempt = 0;
let stopOtp;
let views = [];
let activeView = 0;
let paletteReturnFocus = null;
let fieldJumpReturnFocus = null;
let largeReturnFocus = null;
let searchOrigin = null;

const panes = [groups.closest('.pane'), entries.closest('.pane'), detail];
const sidebarTrigger = document.querySelector('[data-slot="sidebar-trigger"]');
const vaultSwitcher = byId('vault-switcher');
const vaultMenu = byId('vault-menu');
const commandsTrigger = byId('commands');
const commandsMenu = byId('palette');
const vaultInfoDialog = byId('vault-info-dialog');
const vaultInfoFields = byId('vault-info-fields');

vaultMenu.addEventListener('beforetoggle', event => {
  const open = event.newState === 'open';
  vaultSwitcher.setAttribute('aria-expanded', String(open));
  if (!open) return;
  const trigger = vaultSwitcher.getBoundingClientRect();
  const viewportWidth = document.documentElement.clientWidth;
  const minimumWidth = 14 * parseFloat(getComputedStyle(document.documentElement).fontSize);
  const menuWidth = Math.min(Math.max(trigger.width, minimumWidth), viewportWidth - 16);
  const beside = viewportWidth > 740 && trigger.right + menuWidth + 12 <= viewportWidth;
  const left = beside ? trigger.right + 4 : Math.max(8, Math.min(trigger.left, viewportWidth - menuWidth - 8));
  vaultMenu.style.width = `${menuWidth}px`;
  vaultMenu.style.left = `${left}px`;
  vaultMenu.style.top = `${beside ? trigger.top : trigger.bottom + 4}px`;
});

vaultSwitcher.addEventListener('click', event => {
  if (event.detail !== 0 || vaultMenu.matches(':popover-open')) return;
  requestAnimationFrame(() => {
    if (vaultMenu.matches(':popover-open')) byId('open-another').focus();
  });
});

function moveVaultMenuFocus(event) {
  if (!vaultMenu.matches(':popover-open') || (event.key !== 'ArrowDown' && event.key !== 'ArrowUp')) return;
  event.preventDefault();
  const items = [...vaultMenu.querySelectorAll('button')];
  const current = items.indexOf(document.activeElement);
  const direction = event.key === 'ArrowDown' ? 1 : -1;
  const next = current < 0 ? (direction > 0 ? 0 : items.length - 1) : (current + direction + items.length) % items.length;
  items[next].focus();
}

vaultMenu.addEventListener('keydown', moveVaultMenuFocus);
vaultSwitcher.addEventListener('keydown', moveVaultMenuFocus);

function setSidebar(open) {
  sidebarTrigger.setAttribute('aria-expanded', String(open));
  byId('navigation-sidebar').hidden = !open;
}

sidebarTrigger.addEventListener('click', () => setSidebar(sidebarTrigger.getAttribute('aria-expanded') !== 'true'));

function saveView(captureScroll = false) {
  const view = views[activeView];
  if (!view) return;
  Object.assign(view, { group, selected, query: search.value });
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
  ({ group, selected } = view);
  search.value = view.query;
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
  setActionIcon(toggle, visible ? 'eyeOff' : 'eye');
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
    if (lockVault(candidate)) master.focus();
    else vaultFile.value = '';
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
      return 'The vault could not be unlocked. Check the password.';
    case 'FileCorrupt':
    case 'BadSignature':
    case 'InvalidArg':
      return 'The vault is damaged or is not a valid KeePass file.';
    case 'InvalidVersion':
    case 'Unsupported':
    case 'NotImplemented':
      return 'This vault uses a KeePass format or encryption setting the viewer cannot open.';
    default:
      return 'The vault could not be read or decrypted. Check the file and try again.';
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
  const name = file.name;
  const password = ProtectedValue.fromString(master.value);
  master.value = '';
  showMaster(false);
  let credentials;
  try {
    const data = await file.arrayBuffer();
    credentials = new Credentials(password);
    database = await Kdbx.load(data, credentials);
    workspace = vaultWorkspace(database);
    vaultFileInfo = { name, size: file.size, modified: file.lastModified };
    setSidebar(true);
    views = [{ group: null, selected: null, query: '' }]; activeView = 0;
    byId('vault-name').textContent = database.meta.name || name;
    renderGroups();
    renderEntries();
    syncViews();
    resetCredentials();
    byId('unlock-screen').hidden = true;
    byId('viewer').hidden = false;
    search.focus();
  } catch (error) {
    clearVault();
    byId('error').textContent = unlockError(error);
  } finally {
    if (credentials && !database) {
      credentials.setPassword(null);
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
    section.dataset.slot = 'sidebar-group';
    const heading = element('h2', label);
    heading.dataset.slot = 'sidebar-group-label';
    const content = element('div');
    content.dataset.slot = 'sidebar-group-content';
    const list = element('ul');
    list.dataset.slot = 'sidebar-menu';
    content.append(list);
    section.append(heading, content);
    const menus = [list];
    for (const item of items) {
      const depth = item.depth ?? 0;
      if (!menus[depth]) {
        const submenu = element('ul');
        submenu.dataset.slot = 'sidebar-menu-sub';
        menus[depth - 1].lastElementChild.append(submenu);
        menus.push(submenu);
      }
      menus.length = depth + 1;
      const row = element('li');
      row.dataset.slot = depth ? 'sidebar-menu-sub-item' : 'sidebar-menu-item';
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
      node.dataset.slot = depth ? 'sidebar-menu-sub-button' : 'sidebar-menu-button';
      if (item === (group ?? workspace.builtIn[0])) node.setAttribute('aria-current', 'true');
      node.tabIndex = node.hasAttribute('aria-current') ? 0 : -1;
      node.append(element('span', item.name), element('small', String(item.rows.length), 'count'));
      row.append(node);
      menus[depth].append(row);
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
    row.addEventListener('mousedown', event => event.preventDefault());
    row.addEventListener('click', event => {
      if (node.contains(event.target)) return;
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


function copyButton(readValue, label, caption = null) {
  const node = button(undefined, async () => {
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
    if (caption) node.textContent = message;
    else setActionIcon(node, message === 'Copied!' ? 'check' : 'copy');
    copyStatus.textContent = message;
    clearTimeout(copyTimers.get(node));
    copyTimers.set(node, setTimeout(() => {
      if (caption) node.textContent = caption;
      else setActionIcon(node, 'copy');
      copyTimers.delete(node);
    }, 1500));
    clearTimeout(copyStatusTimer);
    copyStatusTimer = setTimeout(() => { copyStatus.textContent = ''; }, 1500);
  });
  node.setAttribute('aria-label', 'Copy ' + label);
  node.title = 'Copy ' + label;
  if (caption) node.textContent = caption;
  else { node.classList.add('field-action'); setActionIcon(node, 'copy'); }
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
  const copy = copyButton(() => displayedStep === currentStep() ? code.textContent : null, 'OTP');
  copy.dataset.fieldKey = 'otp';
  copy.dataset.fieldAction = 'copy';
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
    const contents = element('dd', undefined, 'inline');
    contents.dataset.fieldLabel = label;
    const fieldKey = label === 'Username' ? 'UserName' : label === 'One-time password (TOTP)' ? 'otp' : label;
    contents.dataset.fieldKey = fieldKey; contents.tabIndex = 0;
    value.dataset.fieldValueKey = fieldKey;
    for (const action of actions) {
      if (!action.dataset.fieldAction) action.dataset.fieldAction = 'copy';
      action.dataset.fieldKey = fieldKey;
    }
    if (fieldKey !== 'otp' && value.textContent !== '—') {
      const large = button(undefined, () => openLargeView(fieldKey));
      large.classList.add('field-action');
      large.setAttribute('aria-label', 'Large view of ' + label);
      large.title = 'Large view of ' + label;
      setActionIcon(large, 'large');
      actions.unshift(large);
    }
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
      setActionIcon(toggle, visible ? 'eye' : 'eyeOff');
      toggle.setAttribute('aria-pressed', String(!visible));
    });
    toggle.setAttribute('aria-pressed', 'false');
    toggle.setAttribute('aria-label', 'Reveal or hide ' + label);
    toggle.title = 'Reveal or hide ' + label;
    toggle.classList.add('field-action');
    setActionIcon(toggle, 'eye');
    toggle.dataset.fieldKey = key; toggle.dataset.fieldAction = 'reveal';
    add(label, value, toggle, ...(copy ? [copyButton(() => field(entry, key), label)] : []));
  }
  if (readOnly) {
    add('Title', element('span', field(entry, 'Title') || 'Untitled entry'));
  } else {
    const title = element('input');
    title.type = 'text';
    title.disabled = saving;
    title.value = field(entry, 'Title');
    title.setAttribute('aria-label', 'Edit title');
    let historySaved = false;
    title.addEventListener('input', () => {
      if (!historySaved) { entry.pushHistory(); historySaved = true; }
      const oldTitle = entry.fields.get('Title');
      entry.fields.set('Title', oldTitle instanceof ProtectedValue ? ProtectedValue.fromString(title.value) : title.value);
      entry.times.update();
      dirty = true;
      revision++;
      const row = entries.querySelector('[aria-current]');
      if (row) row.textContent = title.value || 'Untitled entry';
      summary.querySelector('h2').textContent = title.value || 'Untitled entry';
    });
    add('Title', title);
  }
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
  vaultInfoDialog.close();
  vaultInfoFields.replaceChildren();
  vaultFileInfo = null;
  clearOtp();
  database?.credentials?.setPassword(null);
  database = null;
  readOnly = true;
  dirty = false;
  revision = 0;
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
  byId('large-content').textContent = '';
  commandsMenu.replaceChildren();
  byId('tabs').replaceChildren();
}

search.addEventListener('focus', rememberSearch);
search.addEventListener('blur', () => {
  if (!commandsMenu.matches(':popover-open')) searchOrigin = null;
});
search.addEventListener('input', () => {
  if (database) renderEntries();
});
function lockVault(preserveFile = null) {
  if (saving) return false;
  if (dirty && !confirm('Discard unsaved changes?')) return false;
  if (vaultMenu.matches(':popover-open')) vaultMenu.hidePopover();
  closeOverlays();
  views = [];
  activeView = 0;
  clearVault();
  resetCredentials();
  if (preserveFile) chooseFile(preserveFile);
  byId('viewer').hidden = true;
  byId('unlock-screen').hidden = false;
  vaultFile.focus();
  return true;
}
byId('lock').addEventListener('click', () => lockVault());
byId('vault-info').addEventListener('click', showVaultInfo);
vaultInfoDialog.addEventListener('close', () => {
  vaultInfoFields.replaceChildren();
  if (database) vaultSwitcher.focus();
});
installDialogDismissal(document.querySelectorAll('dialog'), dialog => {
  if (dialog === vaultInfoDialog || dialog.id === 'about-dialog') dialog.close();
  else closeOverlays();
});
byId('open-another').addEventListener('click', () => {
  vaultMenu.hidePopover();
  openVault();
});

function openPalette(focusFirst = true) {
  if (byId('large-view').open) closeOverlays();
  if (commandsMenu.matches(':popover-open')) {
    closeOverlays();
    return;
  }
  paletteReturnFocus = document.activeElement;
  hideReveals();
  renderCommands();
  const trigger = commandsTrigger.getBoundingClientRect();
  commandsMenu.showPopover({ source: commandsTrigger });
  const menu = commandsMenu.getBoundingClientRect();
  const gutter = 8;
  commandsMenu.style.left = `${Math.max(gutter, Math.min(trigger.left, document.documentElement.clientWidth - menu.width - gutter))}px`;
  commandsMenu.style.top = `${trigger.bottom + 4}px`;
  commandsMenu.style.maxHeight = `${Math.max(0, document.documentElement.clientHeight - trigger.bottom - 4 - gutter)}px`;
  if (focusFirst) commandsMenu.querySelector('[role^="menuitem"]:not([aria-disabled])')?.focus();
}

function openVault() {
  vaultFile.click();
}

function showVaultInfo() {
  if (!database || !vaultFileInfo) return;
  if (vaultMenu.matches(':popover-open')) vaultMenu.hidePopover();
  const { name, size, modified } = vaultFileInfo;
  const counts = workspace.counts;
  const number = new Intl.NumberFormat();
  const date = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' });
  const fields = [
    ['File', name],
    ['File size', `${number.format(size)} bytes`],
    ['File last modified', modified ? date.format(modified) : 'Unavailable'],
    ['Format', `KDBX ${database.header.versionMajor}.${database.header.versionMinor}`],
    ['Entries', counts.entries],
    ['Favorites', counts.favorites],
    ['Groups', counts.groups],
    ['Tags', counts.tags],
    ['Attachments', counts.attachments],
    ['Entries in Bin', counts.recycled],
  ];
  vaultInfoFields.replaceChildren(...fields.flatMap(([label, value]) => [
    element('dt', label),
    element('dd', typeof value === 'number' ? number.format(value) : value),
  ]));
  vaultInfoDialog.showModal();
}

function closeOverlays() {
  const active = document.activeElement;
  const menuOpen = commandsMenu.matches(':popover-open');
  const returnFocus = menuOpen ? paletteReturnFocus
    : active?.closest('#field-jump') ? fieldJumpReturnFocus
    : active?.closest('#large-view') ? largeReturnFocus : null;
  if (menuOpen) commandsMenu.hidePopover();
  for (const id of ['field-jump', 'large-view']) if (byId(id).open) byId(id).close();
  byId('large-title').textContent = 'Large View';
  byId('large-content').textContent = '';
  byId('large-actions').replaceChildren();
  paletteReturnFocus = null;
  fieldJumpReturnFocus = null;
  largeReturnFocus = null;
  returnFocus?.focus();
}
async function saveVault() {
  if (!database || saving) return;
  const current = database;
  const savedRevision = revision;
  saving = true;
  const titleInput = detail.querySelector('input[aria-label="Edit title"]');
  if (titleInput) titleInput.disabled = true;
  try {
    let handle;
    if (window.showSaveFilePicker) {
      handle = await window.showSaveFilePicker({
        suggestedName: vaultFileInfo.name,
        types: [{ description: 'KeePass database', accept: { 'application/x-keepass2': ['.kdbx'] } }]
      });
    }
    const bytes = await current.save();
    if (handle) {
      const stream = await handle.createWritable();
      try { await stream.write(bytes); await stream.close(); }
      catch (error) { await stream.abort(); throw error; }
    } else {
      const url = URL.createObjectURL(new Blob([bytes], { type: 'application/x-keepass2' }));
      const link = element('a');
      link.href = url;
      link.download = vaultFileInfo.name;
      link.click();
      setTimeout(() => URL.revokeObjectURL(url), 60000);
    }
    if (handle && revision === savedRevision) dirty = false;
  } catch (error) {
    if (error.name !== 'AbortError') alert('Could not save the vault. Your changes are still open.');
  } finally {
    saving = false;
    const currentTitleInput = detail.querySelector('input[aria-label="Edit title"]');
    if (currentTitleInput) currentTitleInput.disabled = false;
  }
}

window.addEventListener('beforeunload', event => {
  if (dirty || saving) { event.preventDefault(); event.returnValue = ''; }
});

function commandGroups() {
  const needsEntry = selected ? undefined : 'Select an entry first.';
  return [
    [['Open vault', 'o', openVault], ['Lock Vault', 'Shift+L', lockVault]],
    [['Read only', '', () => { readOnly = !readOnly; renderDetails(); }, undefined, readOnly],
      ...(!readOnly ? [['Save vault as…', '', saveVault]] : [])],
    [['New Tab', 't', newView], ['Close Tab', '', closeView],
      ['Previous Tab', '[', () => switchView(activeView - 1)], ['Next Tab', ']', () => switchView(activeView + 1)]],
    [['Search', '/', searchAllEntries], ['Jump to field', 'Shift+J', openFieldJump, needsEntry],
      ['Copy focused field', 'c', copyFocusedField, needsEntry], ['Reveal focused field', 'v', revealFocusedField, needsEntry],
      ['Large View', 'Shift+G', openLargeView, needsEntry]],
    [['Toggle dark mode', '', () => byId('theme').click()], ['About', '', () => byId('about-dialog').showModal()]],
  ];
}

function renderCommands() {
  const fragment = document.createDocumentFragment();
  for (const definitions of commandGroups()) {
    if (fragment.childNodes.length) {
      const separator = element('hr');
      separator.setAttribute('role', 'separator');
      fragment.append(separator);
    }
    const group = element('div');
    group.setAttribute('role', 'group');
    for (const [name, keys, run, disabledReason, checked] of definitions) {
      const row = button(name, () => {
        if (disabledReason) return;
        closeOverlays();
        run();
      });
      row.setAttribute('role', checked === undefined ? 'menuitem' : 'menuitemcheckbox');
      if (checked !== undefined) {
        row.setAttribute('aria-checked', String(checked));
        const indicator = element('span', undefined, 'menubar-check');
        indicator.setAttribute('aria-hidden', 'true');
        if (checked) {
          const icon = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
          icon.setAttribute('viewBox', '0 0 24 24');
          icon.setAttribute('fill', 'none');
          icon.setAttribute('stroke', 'currentColor');
          icon.setAttribute('stroke-width', '2');
          icon.setAttribute('stroke-linecap', 'round');
          icon.setAttribute('stroke-linejoin', 'round');
          const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
          path.setAttribute('d', 'M20 6 9 17l-5-5');
          icon.append(path);
          indicator.append(icon);
        }
        row.prepend(indicator);
      }
      row.dataset.slot = 'menubar-item';
      row.tabIndex = -1;
      if (keys) {
        const shortcut = element('kbd', keys);
        shortcut.dataset.slot = 'menubar-shortcut';
        shortcut.setAttribute('aria-hidden', 'true');
        row.append(shortcut);
      }
      if (disabledReason) {
        row.setAttribute('aria-disabled', 'true');
        row.setAttribute('aria-description', disabledReason);
        row.title = disabledReason;
      }
      group.append(row);
    }
    fragment.append(group);
  }
  commandsMenu.replaceChildren(fragment);
}

commandsTrigger.addEventListener('click', event => {
  event.preventDefault();
  openPalette(event.detail === 0);
});
commandsMenu.addEventListener('beforetoggle', event => {
  commandsTrigger.setAttribute('aria-expanded', String(event.newState === 'open'));
});
commandsMenu.addEventListener('toggle', event => {
  if (event.newState === 'closed' && !commandsMenu.matches(':popover-open')) {
    paletteReturnFocus = null;
    if (document.activeElement !== search) searchOrigin = null;
  }
});

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
  node.tabIndex = 0;
  node.focus();
  node.scrollIntoView({ block: 'nearest' });
}
function actionNode(key, action) { return detail.querySelector(`[data-field-action="${action}"][data-field-key="${CSS.escape(key)}"]`); }
function copyFocusedField() {
  const key = activeField() || (selected && selected.fields.has('Password') ? 'Password' : selected && selected.fields.has('UserName') ? 'UserName' : null);
  if (!key) return;
  actionNode(key, 'copy')?.click();
}
function revealFocusedField() {
  const key = activeField() || (selected?.fields.has('Password') ? 'Password' : null);
  if (key) actionNode(key, 'reveal')?.click();
}
function openLargeView(fieldKey) {
  const key = typeof fieldKey === 'string' ? fieldKey : activeField() || (selected?.fields.has('Password') ? 'Password' : selected?.fields.has('UserName') ? 'UserName' : null);
  if (!key) return;
  workspace.recordUse(selected);
  hideReveals();
  const text = actionNode(key, 'reveal') ? field(selected, key) : fieldValueNode(key)?.textContent;
  byId('large-title').textContent = key;
  const content = byId('large-content');
  content.replaceChildren();
  const characters = Array.from(text || '—');
  content.setAttribute('aria-label', text || 'Empty');
  characters.forEach((character, index) => {
    const cell = element('span', undefined, 'large-character');
    cell.setAttribute('aria-hidden', 'true');
    cell.append(element('span', character === ' ' ? ' ' : character), element('small', String(index + 1)));
    content.append(cell);
  });
  const copy = copyButton(() => text, key, 'Copy');
  byId('large-actions').replaceChildren(copy);
  largeReturnFocus = document.activeElement;
  byId('large-view').showModal();
}
function openFieldJump() {
  if (!selected) return;
  fieldJumpReturnFocus = document.activeElement;
  hideReveals();
  byId('field-jump').showModal();
}

function jumpField(letter) {
  const names = { p: 'Password', u: 'UserName', w: 'URL', n: 'Notes', e: 'Email' };
  closeOverlays();
  focusField(names[letter]);
}

function handlePaletteKey(event) {
  const { key } = event;
  if (key === 'Escape' || key === 'Tab') {
    if (key === 'Escape') event.preventDefault();
    closeOverlays();
    return;
  }
  if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(key)) return;
  event.preventDefault();
  const options = [...commandsMenu.querySelectorAll('[role^="menuitem"]:not([aria-disabled])')];
  const current = options.indexOf(document.activeElement);
  const last = options.length - 1;
  if (key === 'Home') options[0]?.focus();
  else if (key === 'End') options[last]?.focus();
  else if (key === 'ArrowDown') options[(current + 1) % options.length]?.focus();
  else options[current <= 0 ? last : current - 1]?.focus();
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
  if (vaultInfoDialog.open || byId('about-dialog').open) return;
  if (vaultMenu.matches(':popover-open')) return;
  const target = event.target;
  const editing = target.matches('input, textarea, select, [contenteditable="true"]');
  if (commandsMenu.matches(':popover-open')) {
    handlePaletteKey(event);
    return;
  }
  if (byId('field-jump').open) {
    if (event.key === 'Tab') { event.preventDefault(); byId('field-jump').querySelector('.overlay-close').focus(); return; }
    if (event.key === 'Escape') { event.preventDefault(); closeOverlays(); }
    else if (/^[puwne]$/i.test(event.key)) { event.preventDefault(); jumpField(event.key.toLowerCase()); }
    return;
  }
  if (byId('large-view').open) {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'p') { event.preventDefault(); closeOverlays(); openPalette(); }
    else if (event.shiftKey && event.key.toLowerCase() === 'l') { event.preventDefault(); lockVault(); }
    else if (event.key === 'Tab') {
      event.preventDefault();
      const close = byId('large-close');
      const copy = byId('large-actions').querySelector('button');
      (document.activeElement === close ? copy : close).focus();
    }
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
  else if (event.key.toLowerCase() === 't') newView();
  else if (event.key === '[') switchView(activeView - 1);
  else if (event.key === ']') switchView(activeView + 1);
  else if (/^[1-9]$/.test(event.key) && views[Number(event.key) - 1]) switchView(Number(event.key) - 1);
});

if (__BYPASS_DEBUG__) {
  const { base64, modified } = __BYPASS_DEBUG_VAULT__;
  const bytes = Uint8Array.from(atob(base64), character => character.charCodeAt(0));
  document.title = 'Bypass Secrets Manager · Debug';
  chooseFile(new File([bytes], 'test-vault.kdbx', { lastModified: modified }));
  master.value = 'password';
  form.requestSubmit();
}
