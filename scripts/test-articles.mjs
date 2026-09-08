import { spawnSync } from "node:child_process";
import { mkdir } from "node:fs/promises";
await import("./build-articles.mjs");
await mkdir("work", { recursive: true });
const binary =
  process.platform === "win32"
    ? "work/article-preview.exe"
    : "work/article-preview";
let result = spawnSync(
  "go",
  ["build", "-o", binary, "./scripts/article-preview"],
  { stdio: "inherit" },
);
if (result.status !== 0) process.exit(result.status || 1);
result = spawnSync(
  process.execPath,
  ["--test", "tests/article-browser.test.mjs"],
  { stdio: "inherit" },
);
process.exit(result.status || 0);
