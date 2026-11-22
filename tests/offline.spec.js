import { expect, test } from '@playwright/test';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import * as kdbx from 'kdbxweb';
import { argon2d, argon2id } from 'hash-wasm';

const viewer = new URL('../dist/index.html', import.meta.url).href;
const password = 'fixture password';
const keyfile = Buffer.alloc(32, 7);
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

async function fixture(version, variant, keyOnly = false) {
  const key = variant ? new Uint8Array(keyfile) : undefined;
  const credentials = new kdbx.Credentials(keyOnly ? null : kdbx.ProtectedValue.fromString(password), key);
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
    ['Argon2id', [4, 'Argon2id', true]]
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

async function openFile(page, buffer, withKey = false) {
  await page.goto(viewer);
  await page.locator('#vault-file').setInputFiles({
    name: 'fixture.kdbx', mimeType: 'application/octet-stream', buffer
  });
  if (withKey) {
    await page.locator('#key-file').setInputFiles({
      name: 'fixture.key', mimeType: 'application/octet-stream', buffer: keyfile
    });
  }
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
    await openFile(page, fixtures.get(name), name !== 'KDBX 3');
    await unlock(page, name === 'Argon2id' ? '' : password);
    await expect(page.getByText('Deleted login')).toHaveCount(0);
    expect(requests).toEqual([viewer]);
    expect(errors).toEqual([]);
  });
}

test('reports wrong credentials and a corrupt file', async ({ page }) => {
  await openFile(page, fixtures.get('KDBX 3'));
  await page.getByLabel('Master password', { exact: true }).fill('wrong');
  await page.getByRole('button', { name: 'Unlock vault' }).click();
  await expect(page.getByRole('alert')).toContainText('Check the password and keyfile');
  await openFile(page, Buffer.alloc(16));
  await page.getByRole('button', { name: 'Unlock vault' }).click();
  await expect(page.getByRole('alert')).toContainText('damaged');
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
  await page.getByRole('button', { name: 'Lock Vault' }).click();
  await expect(page.getByText('359152', { exact: true })).toHaveCount(0);
});

test('release artifact is a single HTML file', async () => {
  const { readdir } = await import('node:fs/promises');
  expect(await readdir(fileURLToPath(new URL('../dist/', import.meta.url)))).toEqual(['index.html']);
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
  const palette = page.getByRole('dialog', { name: 'Command palette' });
  await expect(palette).toBeVisible();
  await expect(palette.getByPlaceholder('Search commands')).toBeFocused();
  await page.keyboard.press('Shift+Tab');
  await expect(palette.getByRole('button', { name: 'Close command palette' })).toBeFocused();
  await page.keyboard.press('Space');
  await expect(palette).toBeHidden();
  await expect(search).toBeFocused();
  await page.keyboard.press('Control+p');
  await page.keyboard.press('Escape');
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
  const detailsSidebar = page.getByRole('button', { name: 'Details', exact: true });
  await detailsSidebar.click();
  await expect(detailsSidebar).toHaveAttribute('aria-expanded', 'false');
  await page.keyboard.press('Shift+j');
  await page.keyboard.press('p');
  await expect(detailsSidebar).toHaveAttribute('aria-expanded', 'true');
  await expect(page.locator('dd[data-field-key="Password"]')).toBeFocused();
  await expect(page.getByText('••••••••', { exact: true })).toBeVisible();
  await page.keyboard.press('c');
  await expect.poll(() => page.evaluate(() => window.copiedValue)).toBe('entry-secret');
  await page.keyboard.press('v');
  await expect(page.getByText('entry-secret', { exact: true })).toBeVisible();
  await page.keyboard.press('Shift+g');
  await expect(page.getByRole('dialog').getByText('entry-secret', { exact: true })).toBeVisible();
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

  await page.getByRole('button', { name: 'Commands' }).click();
  const commandSearch = page.getByPlaceholder('Search commands');
  await commandSearch.fill('close');
  await commandSearch.press('Enter');
  await expect(page.getByRole('tab')).toHaveCount(1);
  await expect(search).toHaveValue('find me');
  await expect(page.getByText('find me in notes', { exact: true })).toBeVisible();
});

test('opens another vault from the keyboard after a file is selected', async ({ page }) => {
  await openFile(page, fixtures.get('KDBX 3'));
  await unlock(page);
  await page.keyboard.press('Escape');
  const picker = page.waitForEvent('filechooser');
  await page.keyboard.press('o');
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

test('opens with a selected entry and navigates TUI views, tags, and groups', async ({ page }) => {
  await openFile(page, fixtures.get('KDBX 3'));
  await unlock(page);
  const nav = page.getByRole('navigation');
  const navigationSidebar = page.getByRole('button', { name: 'Navigation', exact: true });
  await navigationSidebar.click();
  await expect(nav).toBeHidden();
  await navigationSidebar.click();
  await expect(nav).toBeVisible();
  const list = page.getByRole('region', { name: 'Entries', exact: true });
  const entries = list.getByRole('button');
  await expect(entries.first()).toHaveAttribute('aria-current', 'true');
  const table = list.getByRole('table', { name: 'Vault entries' });
  await expect(table.getByRole('columnheader')).toHaveText(['Title', 'Username', 'Website']);
  await table.getByRole('cell', { name: 'example-user', exact: true }).click();
  await expect(table.getByRole('button', { name: 'Example login' })).toBeFocused();
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
  const commands = page.getByPlaceholder('Search commands');
  await commands.fill('Search');
  await commands.press('Enter');
  await expect(entries).toContainText(['Other login']);
});

test('follows the system theme and remembers the header switch preference', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'light' });
  await openFile(page, fixtures.get('KDBX 3'));
  await unlock(page);
  const theme = page.getByRole('switch', { name: 'Dark mode' });
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
