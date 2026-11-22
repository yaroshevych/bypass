import { build } from 'esbuild';
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
const [js, css] = await Promise.all([
  build({ entryPoints: [resolve(source, 'app.js')], bundle: true, minify: true, legalComments: 'inline', write: false, format: 'iife', platform: 'browser', target: ['es2020'], alias: { crypto: resolve(source, 'node-crypto-shim.js') } }),
  build({ entryPoints: [resolve(source, 'styles.css')], bundle: true, minify: true, legalComments: 'inline', write: false, target: ['safari15'] })
]);
const script = js.outputFiles[0].text.replace(/<\/script/gi, '<\\/script');
const stylesheet = css.outputFiles[0].text.replace(/<\/style/gi, '\\3c /style');

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
