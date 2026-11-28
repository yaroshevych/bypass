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
let editingEntry = null;
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
let entryItemReturnFocus = null;
let entryItemEditState = null;
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
  if (editingEntry !== entry) editingEntry = null;
  byId('edit-entry').hidden = readOnly;
  byId('edit-entry').disabled = !entry || saving;
  byId('edit-entry').lastChild.textContent = editingEntry === entry && entry ? ' Done' : ' Edit';
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
  if (!readOnly && editingEntry === entry) {
    let historySaved = false;
    function editable(key, label, multiline = false) {
      const original = entry.fields.get(key);
      const secret = key === 'Password' || original instanceof ProtectedValue;
      const input = element(multiline && !secret ? 'textarea' : 'input');
      if (input.tagName === 'INPUT') input.type = secret ? 'password' : 'text';
      input.disabled = saving;
      input.value = field(entry, key);
      input.setAttribute('aria-label', 'Edit ' + label);
      input.addEventListener('input', () => {
        if (!historySaved) { entry.pushHistory(); historySaved = true; }
        entry.fields.set(key, original instanceof ProtectedValue || key === 'Password'
          ? ProtectedValue.fromString(input.value) : input.value);
        entry.times.update();
        dirty = true;
        byId('unsaved-badge').hidden = false;
        revision++;
        if (key === 'Title') {
          const row = entries.querySelector('[aria-current]');
          if (row) row.textContent = input.value || 'Untitled entry';
          summary.querySelector('h2').textContent = input.value || 'Untitled entry';
        } else if (key === 'UserName') {
          const row = entries.querySelector('[aria-current]')?.closest('tr');
          if (row) row.cells[1].textContent = input.value;
        } else if (key === 'URL') {
          const row = entries.querySelector('[aria-current]')?.closest('tr');
          if (row) row.cells[2].textContent = website(input.value);
        }
      });
      const contents = element('dd', undefined, 'inline');
      if (secret) {
        const group = element('div', undefined, 'secret-group');
        group.setAttribute('role', 'group');
        group.setAttribute('aria-label', label);
        group.dataset.slot = 'button-group';
        input.dataset.slot = 'input';
        group.append(input);
        const toggle = button(undefined, () => {
          const visible = input.type === 'text';
          input.type = visible ? 'password' : 'text';
          toggle.setAttribute('aria-label', (visible ? 'Show ' : 'Hide ') + label);
          toggle.setAttribute('aria-pressed', String(!visible));
          setActionIcon(toggle, visible ? 'eye' : 'eyeOff');
        });
        toggle.classList.add('edit-secret-toggle');
        toggle.setAttribute('aria-label', 'Show ' + label);
        toggle.setAttribute('aria-pressed', 'false');
        setActionIcon(toggle, 'eye');
        group.append(toggle);
        contents.append(group);
      } else {
        contents.append(input);
      }
      list.append(element('dt', label), contents);
    }
    for (const [key, label, multiline] of [
      ['Title', 'title'], ['UserName', 'username'], ['Password', 'password'],
      ['URL', 'URL'], ['Notes', 'notes', true]
    ]) editable(key, label, multiline);
    for (const [key] of entry.fields) {
      if (otpFields.has(key)) editable(key, key);
    }
    detail.append(list);
    renderCustomFieldEditor(entry);
    renderAttachmentEditor(entry);
    return;
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
  if (entry.binaries.size > 0) {
    const attachments = element('section', undefined, 'detail-attachments');
    attachments.append(element('dt', 'Attachments'));
    const names = element('ul');
    for (const [name, binary] of entry.binaries) {
      const item = element('li', undefined, 'attachment-item');
      item.append(element('span', name));
      const preview = button(undefined, () => openAttachmentPreview(name, binary));
      preview.classList.add('field-action');
      preview.setAttribute('aria-label', 'Preview attachment ' + name);
      preview.title = 'Preview attachment';
      setActionIcon(preview, 'eye');
      item.append(preview);
      names.append(item);
    }
    attachments.append(names);
    detail.append(attachments);
  }
}

function renderAttachmentEditor(entry) {
  const section = element('section', undefined, 'detail-attachments attachment-editor');
  section.append(element('dt', 'Attachments'));
  const names = element('ul');
  for (const [name] of entry.binaries) {
    const item = element('li', undefined, 'attachment-item');
    item.append(element('span', name));
    const preview = button(undefined, () => openAttachmentPreview(name, entry.binaries.get(name)));
    preview.classList.add('field-action');
    preview.setAttribute('aria-label', `Preview attachment ${name}`);
    setActionIcon(preview, 'eye');
    const edit = button(undefined, () => openEntryItemEditor(entry, 'attachment', name));
    edit.classList.add('field-action');
    edit.setAttribute('aria-label', `Edit attachment ${name}`);
    setActionIcon(edit, 'edit');
    const remove = button(undefined, () => {
      if (!confirm(`Remove attachment “${name}”?`)) return;
      entry.pushHistory();
      entry.binaries.delete(name);
      markEntryChanged(entry);
      renderDetails();
    });
    remove.classList.add('field-action');
    remove.setAttribute('aria-label', `Remove attachment ${name}`);
    remove.title = 'Remove attachment';
    setActionIcon(remove, 'trash');
    const actions = element('div', undefined, 'detail-item-actions');
    actions.append(preview, edit, remove);
    item.append(actions);
    names.append(item);
  }
  section.append(names);
  section.append(button('Add attachment', () => openEntryItemEditor(entry, 'attachment')));
  detail.append(section);
}

function renderCustomFieldEditor(entry) {
  const section = element('section', undefined, 'detail-attachments custom-field-editor');
  section.append(element('dt', 'Custom fields'));
  const fields = element('dl');
  for (const [name, original] of entry.fields) {
    if (standardFields.has(name) || otpFields.has(name)) continue;
    const row = element('dd', undefined, 'inline custom-field-display');
    const value = original instanceof ProtectedValue ? '••••••••' : (original || '—');
    row.append(element('span', value));
    const edit = button(undefined, () => openEntryItemEditor(entry, 'field', name));
    edit.classList.add('field-action');
    edit.setAttribute('aria-label', `Edit custom field ${name}`);
    setActionIcon(edit, 'edit');
    const remove = button(undefined, () => {
      if (!confirm(`Remove custom field “${name}”?`)) return;
      entry.pushHistory();
      entry.fields.delete(name);
      markEntryChanged(entry);
      renderDetails();
    });
    remove.classList.add('field-action');
    remove.setAttribute('aria-label', `Remove custom field ${name}`);
    remove.title = 'Remove field';
    setActionIcon(remove, 'trash');
    const actions = element('div', undefined, 'detail-item-actions');
    actions.append(edit, remove);
    row.append(actions);
    fields.append(element('dt', name), row);
  }
  section.append(fields, button('Add custom field', () => openEntryItemEditor(entry, 'field')));
  detail.append(section);
}

function markEntryChanged(entry) {
  entry.times.update();
  dirty = true;
  byId('unsaved-badge').hidden = false;
  revision++;
}

function openEntryItemEditor(entry, kind, name = null) {
  entryItemEditState = { entry, kind, originalName: name };
  entryItemReturnFocus = document.activeElement;
  const dialog = byId('entry-item-editor');
  const body = byId('entry-item-editor-fields');
  body.replaceChildren();
  byId('entry-item-editor-title').textContent = `${name ? 'Edit' : 'Add'} ${kind === 'field' ? 'custom field' : 'attachment'}`;
  const nameInput = element('input');
  nameInput.value = name || '';
  nameInput.required = true;
  nameInput.setAttribute('aria-label', kind === 'field' ? 'Custom field name' : 'Attachment name');
  const nameLabel = element('label', kind === 'field' ? 'Name' : 'File name');
  nameLabel.append(nameInput);
  body.append(nameLabel);
  if (kind === 'field') {
    const current = name ? entry.fields.get(name) : null;
    const valueInput = element('textarea');
    valueInput.value = name ? field(entry, name) : '';
    valueInput.setAttribute('aria-label', 'Custom field value');
    const valueLabel = element('label', 'Value');
    valueLabel.append(valueInput);
    body.append(valueLabel);
    const isProtected = current instanceof ProtectedValue;
    const protectLabel = element('label', 'Protect value');
    const protect = element('input');
    protect.type = 'checkbox';
    protect.checked = isProtected;
    protect.setAttribute('aria-label', 'Protect custom field');
    protect.addEventListener('change', () => valueInput.classList.toggle('protected-input', protect.checked));
    protectLabel.prepend(protect);
    body.append(protectLabel);
    valueInput.classList.toggle('protected-input', isProtected);
    byId('entry-item-save').onclick = () => {
      const nextName = nameInput.value.trim();
      if (!nextName || (nextName !== name && entry.fields.has(nextName))) {
        nameInput.setCustomValidity(!nextName ? 'Enter a field name.' : 'A field with this name already exists.');
        nameInput.reportValidity();
        return;
      }
      entry.pushHistory();
      if (name) entry.fields.delete(name);
      const shouldProtect = protect.checked;
      entry.fields.set(nextName, shouldProtect ? ProtectedValue.fromString(valueInput.value) : valueInput.value);
      markEntryChanged(entry);
      dialog.close(); renderDetails();
    };
  } else {
    let file = null;
    if (name) nameInput.value = name;
    else {
      const fileInput = element('input');
      fileInput.type = 'file';
      fileInput.setAttribute('aria-label', 'Choose attachment file');
      fileInput.addEventListener('change', () => {
        file = fileInput.files[0] || null;
        if (file && !nameInput.value) nameInput.value = file.name;
      });
      const fileLabel = element('label', 'File');
      fileLabel.append(fileInput);
      body.append(fileLabel);
    }
    byId('entry-item-save').onclick = async () => {
      const nextName = nameInput.value.trim();
      if (!nextName || (nextName !== name && entry.binaries.has(nextName))) {
        nameInput.setCustomValidity(!nextName ? 'Enter a file name.' : 'An attachment with this name already exists.');
        nameInput.reportValidity();
        return;
      }
      try {
        let value;
        if (name) value = entry.binaries.get(name);
        else {
          if (!file) { alert('Choose an attachment file.'); return; }
          value = await database.createBinary(await file.arrayBuffer());
        }
        entry.pushHistory();
        if (name) entry.binaries.delete(name);
        entry.binaries.set(nextName, value);
        markEntryChanged(entry);
        dialog.close(); renderDetails();
      } catch {
        alert('Could not add the attachment.');
      }
    };
  }
  dialog.showModal();
  nameInput.focus();
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
  byId('unsaved-badge').hidden = true;
  revision = 0;
  workspace = null;
  searchOrigin = null;
  group = null;
  selected = null;
  editingEntry = null;
  byId('edit-entry').hidden = true;
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
byId('edit-entry').addEventListener('click', () => {
  if (readOnly || !selected || saving) return;
  const wasEditing = editingEntry === selected;
  editingEntry = wasEditing ? null : selected;
  renderDetails();
  if (wasEditing) { byId('edit-entry').focus(); return; }
  const input = detail.querySelector('input[aria-label="Edit title"]');
  input?.focus();
  input?.select();
});
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
byId('attachment-preview').addEventListener('close', () => {
  const url = byId('attachment-preview-url').dataset.url;
  if (url) URL.revokeObjectURL(url);
  delete byId('attachment-preview-url').dataset.url;
  byId('attachment-preview-content').replaceChildren();
  largeReturnFocus?.focus();
});
const previewResize = byId('attachment-preview-resize');
previewResize.addEventListener('pointerdown', event => {
  if (event.button !== 0) return;
  event.preventDefault();
  const dialog = byId('attachment-preview');
  const start = dialog.getBoundingClientRect();
  const originX = event.clientX, originY = event.clientY;
  dialog.style.margin = '0';
  dialog.style.position = 'fixed';
  dialog.style.left = `${start.left}px`;
  dialog.style.top = `${start.top}px`;
  previewResize.setPointerCapture(event.pointerId);
  const move = moveEvent => {
    const width = Math.max(320, Math.min(innerWidth - 32, start.width + moveEvent.clientX - originX));
    const height = Math.max(240, Math.min(innerHeight - 32, start.height + moveEvent.clientY - originY));
    dialog.style.width = `${width}px`;
    dialog.style.height = `${height}px`;
  };
  const finish = () => {
    previewResize.removeEventListener('pointermove', move);
    previewResize.removeEventListener('pointerup', finish);
    previewResize.removeEventListener('pointercancel', finish);
  };
  previewResize.addEventListener('pointermove', move);
  previewResize.addEventListener('pointerup', finish);
  previewResize.addEventListener('pointercancel', finish);
});
const previewTitle = byId('attachment-preview-title');
previewTitle.addEventListener('pointerdown', event => {
  if (event.button !== 0) return;
  const dialog = byId('attachment-preview');
  const start = dialog.getBoundingClientRect();
  const originX = event.clientX, originY = event.clientY;
  previewTitle.setPointerCapture(event.pointerId);
  const move = moveEvent => {
    const left = Math.max(0, Math.min(innerWidth - start.width, start.left + moveEvent.clientX - originX));
    const top = Math.max(0, Math.min(innerHeight - 48, start.top + moveEvent.clientY - originY));
    dialog.style.margin = '0';
    dialog.style.position = 'fixed';
    dialog.style.left = `${left}px`;
    dialog.style.top = `${top}px`;
  };
  const finish = () => {
    previewTitle.removeEventListener('pointermove', move);
    previewTitle.removeEventListener('pointerup', finish);
    previewTitle.removeEventListener('pointercancel', finish);
  };
  previewTitle.addEventListener('pointermove', move);
  previewTitle.addEventListener('pointerup', finish);
  previewTitle.addEventListener('pointercancel', finish);
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
    : active?.closest('#large-view, #attachment-preview') ? largeReturnFocus
    : active?.closest('#entry-item-editor') ? entryItemReturnFocus : null;
  if (menuOpen) commandsMenu.hidePopover();
  for (const id of ['field-jump', 'large-view', 'attachment-preview', 'entry-item-editor']) if (byId(id).open) byId(id).close();
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
  for (const input of detail.querySelectorAll('input, textarea')) input.disabled = true;
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
    if (handle && revision === savedRevision) {
      dirty = false;
      byId('unsaved-badge').hidden = true;
    }
  } catch (error) {
    if (error.name !== 'AbortError') alert('Could not save the vault. Your changes are still open.');
  } finally {
    saving = false;
    for (const input of detail.querySelectorAll('input, textarea')) input.disabled = false;
  }
}

window.addEventListener('beforeunload', event => {
  if (dirty || saving) { event.preventDefault(); event.returnValue = ''; }
});

function commandGroups() {
  const needsEntry = selected ? undefined : 'Select an entry first.';
  return [
    [['Open vault', 'o', openVault], ['Lock Vault', 'Shift+L', lockVault]],
    [['Read only', '', () => { readOnly = !readOnly; editingEntry = null; renderDetails(); }, undefined, readOnly],
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
function openAttachmentPreview(name, binary) {
  const value = binary?.value ?? binary;
  const bytes = value instanceof ProtectedValue ? value.getBinary() : value;
  if (!(bytes instanceof ArrayBuffer) && !ArrayBuffer.isView(bytes)) return;
  const data = bytes instanceof ArrayBuffer
    ? new Uint8Array(bytes)
    : new Uint8Array(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const extension = name.split('.').pop().toLowerCase();
  const types = { png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif', webp: 'image/webp', svg: 'image/svg+xml', pdf: 'application/pdf', txt: 'text/plain', md: 'text/markdown', csv: 'text/csv', json: 'application/json', xml: 'application/xml', html: 'text/html', htm: 'text/html', log: 'text/plain', yaml: 'text/yaml', yml: 'text/yaml' };
  const starts = (...signature) => signature.every((byte, index) => data[index] === byte);
  const type = starts(0x25, 0x50, 0x44, 0x46) ? 'application/pdf'
    : starts(0x89, 0x50, 0x4e, 0x47) ? 'image/png'
    : starts(0xff, 0xd8, 0xff) ? 'image/jpeg'
    : starts(0x47, 0x49, 0x46, 0x38) ? 'image/gif'
    : data.length >= 12 && String.fromCharCode(...data.subarray(0, 4)) === 'RIFF' && String.fromCharCode(...data.subarray(8, 12)) === 'WEBP' ? 'image/webp'
    : types[extension] || 'application/octet-stream';
  const content = byId('attachment-preview-content');
  content.replaceChildren();
  byId('attachment-preview-title').textContent = name;
  const blob = new Blob([bytes], { type });
  const url = URL.createObjectURL(blob);
  byId('attachment-preview-url').dataset.url = url;
  let preview;
  if (type.startsWith('image/')) {
    preview = element('img'); preview.src = url; preview.alt = name;
  } else if (type === 'application/pdf') {
    preview = element('iframe'); preview.src = url; preview.title = name;
  } else if (type.startsWith('text/') || type === 'application/json' || type === 'application/xml' || looksLikeText(data)) {
    preview = element('pre', new TextDecoder('utf-8', { fatal: true }).decode(data));
  } else {
    preview = element('p', 'Preview is not available for this file type.', 'muted');
  }
  content.append(preview);
  largeReturnFocus = document.activeElement;
  byId('attachment-preview').showModal();
}
function looksLikeText(data) {
  try {
    const text = new TextDecoder('utf-8', { fatal: true }).decode(data);
    return !text.includes('\0') && (text.length === 0 || [...text].filter(char => {
      const code = char.codePointAt(0);
      return code === 9 || code === 10 || code === 13 || code >= 32;
    }).length / text.length > 0.95);
  } catch {
    return false;
  }
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
