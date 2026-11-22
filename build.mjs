import { build } from 'esbuild';
import { execFile } from 'node:child_process';
import { createRequire } from 'node:module';
import { promisify } from 'node:util';
import { mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = dirname(fileURLToPath(import.meta.url));
const source = resolve(root, 'src');
const output = resolve(root, 'dist');
await rm(output, { recursive: true, force: true });
await mkdir(output, { recursive: true });
const htmlPath = resolve(source, 'index.html');
const html = await readFile(htmlPath, 'utf8');
const cliPackage = createRequire(import.meta.url).resolve('@tailwindcss/cli/package.json');
const { bin } = JSON.parse(await readFile(cliPackage, 'utf8'));
const tailwind = resolve(dirname(cliPackage), bin.tailwindcss);
const geist = dirname(fileURLToPath(import.meta.resolve('geist/font')));
const [js, { stdout: css }, sans, mono, geistLicense, shadcnLicense] = await Promise.all([
  build({ entryPoints: [resolve(source, 'app.js')], bundle: true, minify: true, legalComments: 'inline', write: false, format: 'iife', platform: 'browser', target: ['es2020'], alias: { crypto: resolve(source, 'node-crypto-shim.js') } }),
  promisify(execFile)(process.execPath, [tailwind, '-i', resolve(source, 'styles.css'), '--minify'], { cwd: root }),
  readFile(resolve(geist, 'fonts/geist-sans/Geist-Variable.woff2')),
  readFile(resolve(geist, 'fonts/geist-mono/GeistMono-Regular.woff2')),
  readFile(resolve(geist, '../LICENSE.txt'), 'utf8'),
  readFile(resolve(root, 'licenses/shadcn-ui.txt'), 'utf8'),
]);
const script = js.outputFiles[0].text.replace(/<\/script/gi, '<\\/script');
const fontFace = (family, file, weight) =>
  `@font-face{font-family:"${family}";src:url(data:font/woff2;base64,${file.toString('base64')}) format("woff2");font-weight:${weight};font-display:swap;}`;
const stylesheet = [
  `/*!\n${shadcnLicense}\n${geistLicense}\n*/`,
  fontFace('Geist', sans, '100 900'),
  fontFace('Geist Mono', mono, '400'),
  css
].join('\n').replace(/<\/style/gi, '\\3c /style');

function replaceExactlyOnce(input, pattern, replacement, name) {
  const matches = input.match(pattern);
  if (!matches || matches.length !== 1) {
    throw new Error(`Expected exactly one ${name} placeholder in src/index.html; found ${matches?.length ?? 0}`);
  }
  return input.replace(pattern, () => replacement);
}

let bundled = replaceExactlyOnce(
  html,
  /<link rel="stylesheet" href="\.\/styles\.css">/g,
  `<style>\n${stylesheet}\n</style>`,
  'stylesheet'
);
bundled = replaceExactlyOnce(
  bundled,
  /<script type="module" src="\.\/app\.js"><\/script>/g,
  `<script>\n${script}\n</script>`,
  'application script'
);
await writeFile(resolve(output, 'index.html'), bundled);
