import { expect, test } from '@playwright/test';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import { argon2d, argon2id } from 'hash-wasm';

const viewer = new URL('../dist/index.html', import.meta.url).href;
const kdbx = createRequire(import.meta.url)('kdbxweb');
const password = 'fixture password';
const fixtures = new Map();
const sha1Seed = 'GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ';
const sha256Seed = 'GEZDGNBVGY3TQOJQ'.repeat(3) + 'GEZA====';
const sha512Seed = 'GEZDGNBVGY3TQOJQ'.repeat(6) + 'GEZDGNA=';

// Give the fixture generator the same DOM API entry points the browser supplies.
const { DOMParser, XMLSerializer } = createRequire(import.meta.resolve('kdbxweb'))('@xmldom/xmldom');
Object.assign(globalThis, { DOMParser, XMLSerializer });

kdbx.CryptoEngine.setArgon2Impl(async (password, salt, memorySize, iterations, hashLength, parallelism, type) => {
  const hash = type === 0 ? argon2d : argon2id;
  return (await hash({
    password: new Uint8Array(password), salt: new Uint8Array(salt),
    memorySize, iterations, hashLength, parallelism, outputType: 'binary'
  })).buffer;
});

async function fixture(version, variant, includeAttachments = false) {
  const credentials = new kdbx.Credentials(kdbx.ProtectedValue.fromString(password));
  const database = kdbx.Kdbx.create(credentials, 'Test vault');
  database.setVersion(version);
  if (variant) {
    database.setKdf(kdbx.Consts.KdfId[variant]);
    const { UInt64 } = kdbx.VarDictionary.ValueType;
    database.header.kdfParameters.set('M', UInt64, kdbx.Int64.from(64 * 1024));
    database.header.kdfParameters.set('I', UInt64, kdbx.Int64.from(2));
  }

  const root = database.getDefaultGroup();
  const group = database.createGroup(root, 'Work');
  const entry = database.createEntry(group);
  entry.fields.set('Title', 'Example login');
  entry.tags = ['Favorite', 'work'];
  entry.fields.set('UserName', 'example-user');
  entry.fields.set('Password', kdbx.ProtectedValue.fromString('entry-secret'));
  entry.fields.set('Notes', 'find me in notes');
  entry.fields.set('URL', 'javascript:alert(1)');
  entry.fields.set('Literal', '<img src="https://example.invalid/secret">');
  entry.fields.set('otp', kdbx.ProtectedValue.fromString(
    'otpauth://totp/Example?secret=' + sha1Seed + '&digits=8'
  ));
  if (includeAttachments) {
    const image = Uint8Array.from(atob('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p7sAAAAASUVORK5CYII='), char => char.charCodeAt(0));
    entry.binaries.set('pixel.png', await database.createBinary(image));
    entry.binaries.set('sample.pdf', await database.createBinary(new TextEncoder().encode('%PDF-1.4\nfixture pdf\n%%EOF')));
    entry.binaries.set('id_ed25519', await database.createBinary(new TextEncoder().encode('-----BEGIN OPENSSH PRIVATE KEY-----\nfixture private key text\n-----END OPENSSH PRIVATE KEY-----')));
  }
  const other = database.createEntry(root);
  other.fields.set('Title', 'Other login');
  other.fields.set('TimeOtp-Secret-Base32', sha1Seed);
  for (const [hash, seed] of [['SHA-256', sha256Seed], ['SHA-512', sha512Seed]]) {
    const vector = database.createEntry(root);
    vector.fields.set('Title', hash);
    vector.fields.set('TimeOtp-Secret-Base32', seed);
    vector.fields.set('TimeOtp-Algorithm', 'HMAC-' + hash);
    vector.fields.set('TimeOtp-Length', '8');
  }
  database.createRecycleBin();
  const bin = root.groups.find(group => group.uuid.id === database.meta.recycleBinUuid.id);
  database.createEntry(bin).fields.set('Title', 'Deleted login');
  return Buffer.from(await database.save());
}

test.beforeAll(async () => {
  for (const [name, args] of [
    ['KDBX 3', [3]],
    ['Argon2d', [4, 'Argon2d']],
    ['Argon2id', [4, 'Argon2id']]
  ]) {
    fixtures.set(name, await fixture(...args));
  }
});

test.beforeEach(async ({ page, context }) => {
  await context.setOffline(true);
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: async value => { window.copiedValue = value; } }
    });
  });
});

async function openFile(page, buffer) {
  await page.goto(viewer);
  await page.locator('#vault-file').setInputFiles({
    name: 'fixture.kdbx', mimeType: 'application/octet-stream', buffer
  });
}

async function unlock(page, value = password) {
  await page.getByLabel('Master password', { exact: true }).fill(value);
  await page.getByRole('button', { name: 'Unlock vault' }).click();
  await expect(page.getByRole('heading', { name: 'Test vault' })).toBeVisible();
}

for (const name of ['KDBX 3', 'Argon2d', 'Argon2id']) {
  test(`opens ${name} offline without network requests`, async ({ page }) => {
    const requests = [];
    const errors = [];
    page.on('request', request => requests.push(request.url()));
    page.on('pageerror', error => errors.push(error.message));
    await openFile(page, fixtures.get(name));
    await unlock(page);
    await expect(page.getByText('Deleted login')).toHaveCount(0);
    expect(requests).toEqual([viewer]);
    expect(errors).toEqual([]);
  });
}

test('reports wrong credentials and a corrupt file', async ({ page }) => {
  await openFile(page, fixtures.get('KDBX 3'));
  await page.getByLabel('Master password', { exact: true }).fill('wrong');
  await page.getByRole('button', { name: 'Unlock vault' }).click();
  await expect(page.getByRole('alert')).toContainText('Check the password');
  await openFile(page, Buffer.alloc(16));
  await page.getByRole('button', { name: 'Unlock vault' }).click();
  await expect(page.getByRole('alert')).toContainText('damaged');
});

test('previews and edits attachments, then saves them to the vault', async ({ page }) => {
  await page.addInitScript(() => {
    window.showSaveFilePicker = async () => ({ createWritable: async () => ({
      write: async bytes => { window.savedVault = Array.from(new Uint8Array(bytes)); },
      close: async () => {}, abort: async () => {}
    }) });
  });
  await openFile(page, await fixture(4, 'Argon2id', true));
  await unlock(page);
  await page.getByRole('button', { name: 'Example login' }).click();

  await page.getByRole('button', { name: 'Preview attachment pixel.png' }).click();
  await expect(page.locator('#attachment-preview img')).toHaveAttribute('src', /^blob:/);
  await page.getByRole('button', { name: 'Close' }).last().click();
  await page.getByRole('button', { name: 'Preview attachment sample.pdf' }).click();
  await expect(page.locator('#attachment-preview iframe')).toHaveAttribute('src', /^blob:/);
  await page.getByRole('button', { name: 'Close' }).last().click();
  await page.getByRole('button', { name: 'Preview attachment id_ed25519' }).click();
  await expect(page.locator('#attachment-preview pre')).toContainText('BEGIN OPENSSH PRIVATE KEY');
  await page.getByRole('button', { name: 'Close' }).last().click();

  await page.getByRole('button', { name: 'Menu' }).click();
  await page.getByRole('menuitemcheckbox', { name: /Read only/ }).click();
  await page.getByRole('button', { name: 'Edit', exact: true }).click();
  const keyName = page.getByLabel('Rename attachment id_ed25519');
  await keyName.fill('deploy_key.txt');
  await page.getByRole('button', { name: 'Rename' }).last().click();
  page.once('dialog', dialog => dialog.accept());
  await page.getByLabel('Rename attachment sample.pdf').locator('..').getByRole('button', { name: 'Remove' }).click();
  await page.getByLabel('Add attachments').setInputFiles({ name: 'added.txt', mimeType: 'text/plain', buffer: Buffer.from('new attachment') });
  await expect(page.getByLabel('Rename attachment added.txt')).toBeVisible();
  await page.getByRole('button', { name: 'Done', exact: true }).click();
  await page.getByRole('button', { name: 'Menu' }).click();
  await page.getByRole('menuitem', { name: 'Save vault as…' }).click();
  await expect.poll(() => page.evaluate(() => window.savedVault?.length || 0)).toBeGreaterThan(0);

  const bytes = Uint8Array.from(await page.evaluate(() => window.savedVault));
  const saved = await kdbx.Kdbx.load(bytes.buffer, new kdbx.Credentials(kdbx.ProtectedValue.fromString(password)));
  const savedEntry = saved.getDefaultGroup().groups.find(group => group.name === 'Work').entries[0];
  expect([...savedEntry.binaries.keys()].sort()).toEqual(['added.txt', 'deploy_key.txt', 'pixel.png']);
  const added = savedEntry.binaries.get('added.txt');
  expect(new TextDecoder().decode(added.value instanceof kdbx.ProtectedValue ? added.value.getBinary() : added.value)).toBe('new attachment');
});

test('shows and copies a live OTP without exposing its seed or rendering entry HTML', async ({ page }) => {
  await page.clock.setFixedTime(new Date(59_000));
  await openFile(page, fixtures.get('KDBX 3'));
  await unlock(page);
  await page.getByRole('button', { name: /Example login/ }).click();
  await expect(page.getByText('94287082', { exact: true })).toBeVisible();
  await expect(page.getByText('1s remaining')).toBeVisible();
  await page.getByRole('button', { name: 'Copy OTP' }).click();
  await expect.poll(() => page.evaluate(() => window.copiedValue)).toBe('94287082');
  await page.clock.setFixedTime(new Date(60_000));
  await expect(page.getByText('37359152', { exact: true })).toBeVisible();
  await expect(page.getByText('GEZDGNBVGY3TQOJQ', { exact: false })).toHaveCount(0);
  await expect(page.getByText('<img src="https://example.invalid/secret">', { exact: true })).toBeVisible();
  await expect(page.locator('img')).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'javascript:alert(1)' })).toHaveCount(0);
  await page.getByRole('button', { name: /Other login/ }).click();
  await expect(page.getByText('359152', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Test vault', exact: true }).click();
  await page.getByRole('button', { name: 'Lock Vault' }).click();
  await expect(page.getByText('359152', { exact: true })).toHaveCount(0);
});

test('release is a single HTML file with fonts that load offline', async ({ page }) => {
  const { readdir } = await import('node:fs/promises');
  expect(await readdir(fileURLToPath(new URL('../dist/', import.meta.url)))).toEqual(['index.html']);
  await page.goto(viewer);
  await expect(page.locator('body')).toHaveCSS('font-family', /^Geist,/);
  const loaded = await page.evaluate(async () => {
    const fonts = await Promise.all([
      document.fonts.load('500 14px Geist'),
      document.fonts.load('400 14px "Geist Mono"'),
    ]);
    return fonts.map(faces => faces.map(face => face.status));
  });
  expect(loaded).toEqual([['loaded'], ['loaded']]);
});

test('matches the RFC 6238 Appendix B test vectors', async ({ page }) => {
  await openFile(page, fixtures.get('KDBX 3'));
  await unlock(page);
  const seconds = [59, 1111111109, 1111111111, 1234567890, 2000000000, 20000000000];
  const vectors = [
    [/Example login/, ['94287082', '07081804', '14050471', '89005924', '69279037', '65353130']],
    [/SHA-256/, ['46119246', '68084774', '67062674', '91819424', '90698825', '77737706']],
    [/SHA-512/, ['90693936', '25091201', '99943326', '93441116', '38618901', '47863826']]
  ];
  for (const [entry, expected] of vectors) {
    await page.getByRole('button', { name: entry }).click();
    for (const [index, time] of seconds.entries()) {
      await page.clock.setFixedTime(new Date(time * 1000));
      await expect(page.getByText(expected[index], { exact: true })).toBeVisible();
    }
  }
});

test('uses the palette and keyboard to search, copy, reveal, and lock', async ({ page }) => {
  await openFile(page, fixtures.get('KDBX 3'));
  const master = page.getByLabel('Master password', { exact: true });
  await master.fill(password);
  await master.press('Enter');
  await expect(page.getByRole('heading', { name: 'Test vault' })).toBeVisible();

  const search = page.getByRole('searchbox', { name: 'Search entries' });
  await search.fill('find me');
  await page.keyboard.press('Control+p');
  const palette = page.getByRole('menu', { name: 'Menu' });
  await expect(palette).toBeVisible();
  await expect(palette.getByRole('menuitem', { name: 'Open vault' })).toBeFocused();
  await expect(palette.getByRole('searchbox')).toHaveCount(0);
  await page.keyboard.press('ArrowDown');
  await expect(palette.getByRole('menuitem', { name: 'Lock Vault' })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(palette).toBeHidden();
  await expect(search).toBeFocused();
  await expect(search).toHaveValue('find me');
  await page.keyboard.press('Escape');
  await expect(search).toHaveValue('');
  await page.keyboard.press('/');
  await expect(search).toBeFocused();
  await search.fill('find me');
  await page.keyboard.press('ArrowDown');
  await expect(page.getByRole('button', { name: /Example login/ })).toBeFocused();
  await page.keyboard.press('Enter');
  await page.keyboard.press('Shift+j');
  await page.keyboard.press('p');
  await expect(page.locator('dd[data-field-key="Password"]')).toBeFocused();
  await expect(page.getByText('••••••••', { exact: true })).toBeVisible();
  await page.keyboard.press('c');
  await expect.poll(() => page.evaluate(() => window.copiedValue)).toBe('entry-secret');
  await page.keyboard.press('v');
  await expect(page.getByText('entry-secret', { exact: true })).toBeVisible();
  await page.keyboard.press('Shift+g');
  await expect(page.locator('#large-content')).toHaveAttribute('aria-label', 'entry-secret');
  await page.keyboard.press('Shift+l');
  await expect(master).toBeVisible();
  await expect(page.getByText('entry-secret', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('tab', { includeHidden: true })).toHaveCount(0);
  await expect(page.locator('#vault-file')).toBeFocused();
});

test('keeps tab selection and search independent across switching and closing', async ({ page }) => {
  await openFile(page, fixtures.get('KDBX 3'));
  await unlock(page);
  const search = page.getByRole('searchbox', { name: 'Search entries' });
  await search.fill('find me');
  await search.press('Enter');
  await page.keyboard.press('t');
  await expect(page.getByRole('tab')).toHaveCount(2);
  await expect(search).toHaveValue('find me');
  await search.fill('other login');
  await search.press('Enter');
  await page.keyboard.press('1');
  await expect(search).toHaveValue('find me');
  await expect(page.getByText('find me in notes', { exact: true })).toBeVisible();
  await page.keyboard.press('2');
  await expect(search).toHaveValue('other login');

  const commands = page.getByRole('button', { name: 'Menu' });
  const menu = page.getByRole('menu', { name: 'Menu' });
  await commands.click();
  await expect(menu).toBeVisible();
  const triggerBox = await commands.boundingBox();
  const menuBox = await menu.boundingBox();
  expect(menuBox.y).toBeGreaterThan(triggerBox.y + triggerBox.height);
  await commands.click();
  await expect(menu).toBeHidden();
  await commands.click();
  await page.getByRole('menuitem', { name: 'Close Tab' }).click();
  await expect(page.getByRole('tab')).toHaveCount(1);
  await expect(search).toHaveValue('find me');
  await expect(page.getByText('find me in notes', { exact: true })).toBeVisible();
});

test('opens another vault from the keyboard after a file is selected', async ({ page }) => {
  await openFile(page, fixtures.get('KDBX 3'));
  await unlock(page);
  const switcher = page.getByRole('button', { name: 'Test vault', exact: true });
  await switcher.press('Enter');
  const openAnother = page.getByRole('button', { name: 'Open another…' });
  await expect(openAnother).toBeFocused();
  const menuBox = await page.locator('#vault-menu').boundingBox();
  const triggerBox = await switcher.boundingBox();
  expect(menuBox.x).toBeGreaterThan(triggerBox.x + triggerBox.width);
  expect(menuBox.y).toBe(triggerBox.y);
  const picker = page.waitForEvent('filechooser');
  await openAnother.press('Enter');
  const chooser = await picker;
  await expect(page.getByRole('heading', { name: 'Test vault' })).toBeVisible();
  await chooser.setFiles({
    name: 'next.kdbx',
    mimeType: 'application/octet-stream',
    buffer: fixtures.get('KDBX 3')
  });
  await expect(page.getByLabel('Master password', { exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Test vault' })).toHaveCount(0);
  await unlock(page);
});

test('shows file metadata and vault counts from the Info menu', async ({ page }) => {
  const fixture = fixtures.get('KDBX 3');
  await openFile(page, fixture);
  await unlock(page);
  const switcher = page.getByRole('button', { name: 'Test vault', exact: true });
  await switcher.click();
  await expect(page.locator('#vault-menu')).toBeVisible();
  await expect(page.locator('#vault-menu :focus')).toHaveCount(0);
  await page.getByRole('button', { name: 'Info', exact: true }).click();
  const info = page.getByRole('dialog', { name: 'Vault information' });
  await expect(info).toBeVisible();
  const value = label => info.getByText(label, { exact: true }).locator('xpath=following-sibling::dd[1]');
  await expect(value('File')).toHaveText('fixture.kdbx');
  await expect(value('File size')).toHaveText(new Intl.NumberFormat().format(fixture.length) + ' bytes');
  await expect(value('File last modified')).not.toHaveText('Unavailable');
  await expect(value('Format')).toHaveText(/^KDBX 3\./);
  await expect(value('Entries')).toHaveText('4');
  await expect(value('Favorites')).toHaveText('1');
  await expect(value('Tags')).toHaveText('1');
  await expect(value('Entries in Bin')).toHaveText('1');
  const close = info.getByRole('button', { name: 'Close' });
  await expect(info.getByRole('heading', { name: 'Vault information' })).toBeFocused();
  await expect(close).toHaveText('');
  await close.click();
  await expect(info).toBeHidden();
  await expect(switcher).toBeFocused();
});

test('opens with a selected entry and navigates TUI views, tags, and groups', async ({ page }) => {
  await openFile(page, fixtures.get('KDBX 3'));
  await unlock(page);
  const nav = page.getByRole('navigation');
  const navigationSidebar = page.getByRole('button', { name: 'Toggle navigation sidebar' });
  const [navBox, leftToggle] = await Promise.all([
    nav.boundingBox(),
    navigationSidebar.boundingBox(),
  ]);
  expect(leftToggle.x).toBeGreaterThan(navBox.x + navBox.width);
  expect(navBox.y + navBox.height - leftToggle.y - leftToggle.height).toBeLessThanOrEqual(12);
  await navigationSidebar.click();
  await expect(nav).toBeHidden();
  await navigationSidebar.click();
  await expect(nav).toBeVisible();
  await expect(nav.getByRole('button', { name: /All Entries/ })).toHaveCSS('height', '28px');
  const workGroup = nav.locator('[data-slot="sidebar-menu-sub"]').getByRole('button', { name: /^Work\s/i }).filter({ hasText: /^Work/ });
  await expect(workGroup).toHaveCSS('height', '28px');
  const list = page.getByRole('region', { name: 'Entries', exact: true });
  const entries = list.getByRole('button');
  await expect(entries.first()).toHaveAttribute('aria-current', 'true');
  const table = list.getByRole('table', { name: 'Vault entries' });
  await expect(table.getByRole('columnheader')).toHaveText(['Title', 'Username', 'Website']);
  await table.getByRole('cell', { name: 'example-user', exact: true }).click();
  await expect(table.getByRole('button', { name: 'Example login' })).toHaveAttribute('aria-current', 'true');
  await expect(table.getByRole('button', { name: 'Example login' })).not.toBeFocused();
  await expect(table.getByRole('button', { name: 'Example login' })).toHaveAttribute('aria-current', 'true');
  await nav.getByRole('button', { name: /All Entries/ }).press('ArrowDown');
  await expect(nav.getByRole('button', { name: /Favorites/ })).toHaveAttribute('aria-current', 'true');
  await expect(entries).toHaveCount(1);
  await expect(page.getByRole('tab', { name: 'Tab 1: Example login' })).toBeVisible();
  await nav.getByRole('button', { name: /Bin/ }).click();
  await expect(entries).toContainText(['Deleted login']);
  await nav.getByRole('button', { name: /^work\s*1$/ }).click();
  await expect(entries).toContainText(['Example login']);
  await page.getByRole('searchbox', { name: 'Search entries' }).fill('other login');
  await expect(entries).toHaveCount(0);
  await page.keyboard.press('Control+p');
  await page.getByRole('menuitem', { name: 'Search', exact: true }).click();
  await expect(entries).toContainText(['Other login']);
});

test('follows the system theme and remembers the floating switch preference', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'light' });
  await openFile(page, fixtures.get('KDBX 3'));
  const theme = page.getByRole('switch', { name: 'Dark mode' });
  await expect(theme).toBeVisible();
  await unlock(page);
  const controls = await page.locator('.vault-header').evaluate(header =>
    [...header.children].map(control => {
      const box = control.getBoundingClientRect();
      return { x: box.x, center: box.y + box.height / 2 };
    })
  );
  expect(controls).toHaveLength(3);
  expect(controls.map(control => control.x)).toEqual(controls.map(control => control.x).sort((a, b) => a - b));
  expect(Math.max(...controls.map(control => control.center)) - Math.min(...controls.map(control => control.center))).toBeLessThan(1);
  const themeBox = await theme.boundingBox();
  expect(themeBox.x + themeBox.width).toBeCloseTo(page.viewportSize().width - 8, 0);
  expect(themeBox.y).toBeCloseTo(8, 0);
  await expect(theme).toHaveAttribute('aria-checked', 'false');
  const background = () => page.locator('body').evaluate(body => getComputedStyle(body).backgroundColor);
  const light = await background();
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect(theme).toHaveAttribute('aria-checked', 'true');
  expect(await background()).not.toEqual(light);
  await theme.press('Space');
  await expect(theme).toHaveAttribute('aria-checked', 'false');
  expect(await background()).toEqual(light);
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
});

for (const name of ['KDBX 3', 'Argon2id']) {
  test(`edits fields and saves a readable ${name} vault`, async ({ page }) => {
    await page.addInitScript(() => {
      window.showSaveFilePicker = async options => {
        window.saveOptions = options;
        return { createWritable: async () => ({
          write: async bytes => { window.savedVault = Array.from(new Uint8Array(bytes)); },
          close: async () => {}, abort: async () => {}
        }) };
      };
    });
    await openFile(page, fixtures.get(name));
    await unlock(page);
    await page.getByRole('button', { name: 'Example login' }).click();
    await expect(page.getByRole('button', { name: 'Edit', exact: true })).toBeHidden();
    await page.getByRole('button', { name: 'Menu' }).click();
    await expect(page.getByRole('menuitemcheckbox', { name: /Read only/ })).toHaveAttribute('aria-checked', 'true');
    await page.getByRole('menuitemcheckbox', { name: /Read only/ }).click();
    await expect(page.getByLabel('Edit title')).toHaveCount(0);
    await page.getByRole('button', { name: 'Edit', exact: true }).click();
    await expect(page.locator('#detail .field-action')).toHaveCount(0);
    await expect(page.locator('#unsaved-badge')).toBeHidden();
    await page.getByLabel('Edit title').fill('Updated login');
    await expect(page.locator('#unsaved-badge')).toBeVisible();
    await page.getByLabel('Edit username').fill('updated-user');
    const passwordInput = page.getByLabel('Edit password');
    await expect(passwordInput).toHaveAttribute('type', 'password');
    await page.getByRole('button', { name: 'Show password' }).click();
    await expect(passwordInput).toHaveAttribute('type', 'text');
    await passwordInput.fill('updated-secret');
    await page.getByRole('button', { name: 'Hide password' }).click();
    await expect(passwordInput).toHaveAttribute('type', 'password');
    await page.getByLabel('Edit notes').fill('updated notes');
    await page.getByLabel('Custom field name Literal').fill('Renamed field');
    await page.getByLabel('Custom field value Literal').fill('updated custom field');
    await page.getByRole('button', { name: 'Save field' }).click();
    await page.getByLabel('New custom field name').fill('Temporary field');
    await page.getByLabel('New custom field value').fill('remove me');
    await page.getByRole('button', { name: 'Add field' }).click();
    page.once('dialog', dialog => dialog.accept());
    const temporary = page.getByLabel('Custom field name Temporary field');
    await temporary.locator('..').getByRole('button', { name: 'Remove' }).click();
    await page.getByLabel('New custom field name').fill('API token');
    await page.getByLabel('New custom field value').fill('custom-secret');
    await page.getByLabel('Protect new custom field').check();
    await page.getByRole('button', { name: 'Add field' }).click();
    await expect(page.getByLabel('Edit otp')).toHaveAttribute('type', 'password');
    await page.getByRole('button', { name: 'Show otp' }).click();
    await expect(page.getByLabel('Edit otp')).toHaveAttribute('type', 'text');
    await page.getByRole('button', { name: 'Done', exact: true }).click();
    await expect(page.getByLabel('Edit title')).toHaveCount(0);
    await page.getByRole('button', { name: 'Menu' }).click();
    await expect(page.getByRole('menuitemcheckbox', { name: /Read only/ })).toHaveAttribute('aria-checked', 'false');
    await page.getByRole('menuitem', { name: 'Save vault as…' }).click();
    await expect.poll(() => page.evaluate(() => window.savedVault?.length || 0)).toBeGreaterThan(0);
    await expect(page.locator('#unsaved-badge')).toBeHidden();
    const bytes = Uint8Array.from(await page.evaluate(() => window.savedVault));
    const saved = await kdbx.Kdbx.load(bytes.buffer, new kdbx.Credentials(kdbx.ProtectedValue.fromString(password)));
    expect(saved.header.versionMajor).toBe(name === 'KDBX 3' ? 3 : 4);
    const entry = saved.getDefaultGroup().groups.find(group => group.name === 'Work').entries[0];
    expect(entry.fields.get('Title')).toBe('Updated login');
    expect(entry.fields.get('UserName')).toBe('updated-user');
    expect(entry.fields.get('Password').getText()).toBe('updated-secret');
    expect(entry.fields.get('Notes')).toBe('updated notes');
    expect(entry.fields.get('Literal')).toBeUndefined();
    expect(entry.fields.get('Renamed field')).toBe('updated custom field');
    expect(entry.fields.get('Temporary field')).toBeUndefined();
    expect(entry.fields.get('API token').getText()).toBe('custom-secret');
    expect(entry.history.some(item => item.fields.get('Title') === 'Example login')).toBe(true);
  });
}
