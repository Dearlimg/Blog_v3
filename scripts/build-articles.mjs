import { build } from "esbuild";
import { readdir, unlink } from "node:fs/promises";
import { resolve } from "node:path";
const result = await build({
  entryPoints: [
    "view/JS/articles/public.js",
    "view/JS/articles/studio.js",
    "view/JS/articles/editor.js",
    "view/JS/articles/home.js",
  ],
  outdir: "static/article-build",
  bundle: true,
  minify: true,
  format: "esm",
  splitting: true,
  target: ["es2022"],
  supported: { "template-literal": false },
  legalComments: "eof",
  metafile: true,
});
const outputs = new Set(
  Object.keys(result.metafile.outputs).map((p) => resolve(p)),
);
for (const name of await readdir("static/article-build")) {
  if (!/^chunk-[A-Z0-9]+\.js$/.test(name)) continue;
  const path = resolve("static/article-build", name);
  if (!outputs.has(path)) await unlink(path);
}
