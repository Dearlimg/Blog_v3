import { build } from 'esbuild';
import { mkdir, writeFile } from 'node:fs/promises';
import { dirname } from 'node:path';

const result = await build({
  entryPoints: ['view/JS/agent.js'],
  outdir: 'static/agent-build',
  bundle: true,
  minify: true,
  format: 'iife',
  target: ['es2022'],
  supported: { 'template-literal': false },
  legalComments: 'eof',
  write: false,
});
for (const file of result.outputFiles) {
  await mkdir(dirname(file.path), { recursive: true });
  await writeFile(file.path, file.text.replace(/[\t ]+$/gm, ''));
}
