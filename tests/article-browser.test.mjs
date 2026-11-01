import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { chromium } from "playwright";

const base = "http://127.0.0.1:18086";
const password = "article-browser-test-password";
let server, browser, temp, ownerToken;
async function request(path, method = "GET", body, token = ownerToken) {
  const response = await fetch(base + "/api/v1" + path, {
    method,
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  return { status: response.status, ...(await response.json()) };
}
async function waitFor(check, message) {
  for (let i = 0; i < 100; i++) {
    if (await check()) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(message);
}
before(async () => {
  temp = await mkdtemp(join(tmpdir(), "article-browser-"));
  await mkdir("work/screenshots", { recursive: true });
  server = spawn(
    process.platform === "win32"
      ? "work/article-preview.exe"
      : "work/article-preview",
    ["-listen", "127.0.0.1:18086", "-db", join(temp, "test.db")],
    {
      env: { ...process.env, ARTICLE_PREVIEW_PASSWORD: password },
      stdio: "pipe",
    },
  );
  let logs = "";
  server.stderr.on("data", (d) => (logs += d));
  server.on("error", (e) => (logs += e.message));
  await waitFor(async () => {
    try {
      return (await fetch(base + "/articles")).ok;
    } catch {
      return false;
    }
  }, "preview did not start: " + logs);
  ownerToken = (
    await request(
      "/users/login",
      "POST",
      { email: "owner@example.test", password },
      null,
    )
  ).data.token;
  browser = await chromium.launch({
    headless: true,
    ...(process.env.PLAYWRIGHT_CHANNEL
      ? { channel: process.env.PLAYWRIGHT_CHANNEL }
      : {}),
  });
});
after(async () => {
  await browser?.close();
  if (server) {
    const ended = new Promise((resolve) => server.once("exit", resolve));
    server.kill();
    await ended;
  }
  if (temp) await rm(temp, { recursive: true, force: true });
});
async function pageForOwner(options = {}) {
  const context = await browser.newContext(options);
  await context.addInitScript(
    (token) => localStorage.setItem("token", token),
    ownerToken,
  );
  const page = await context.newPage();
  return { context, page };
}
async function editBody(page, body) {
  await page.locator(".cm-content").fill(body);
}
async function synced(page) {
  await page.waitForFunction(() =>
    document
      .querySelector("#save-status")
      ?.textContent.startsWith("Synced with server"),
  );
}

test("editor: publish isolation, upload, snapshot restore, local recovery, conflicts and trash", async () => {
  const { context, page } = await pageForOwner({
    viewport: { width: 1440, height: 1000 },
  });
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const id = crypto.randomUUID();
  await page.goto(base + `/studio/articles/${id}/edit`);
  await page.locator(".cm-content").waitFor();
  await synced(page);
  await page.locator("#template").click();
  assert.equal(await page.locator("#category").inputValue(), "interview");
  assert.ok((await page.locator(".cm-content").innerText()).includes("Knowledge gaps"));
  await page.locator("#title").fill("Go 并发面试复盘");
  await page.locator("#summary").fill("记录并发、锁与项目实践中的关键问题。");
  await page.locator("#tags").fill("Go, 并发");
  await page.locator("#private-notes").fill("PRIVATE COMPANY / offer result");
  await editBody(
    page,
    "## 并发问题\n\n如何避免数据竞争？\n\n```go\nvar count int\n```\n\n## 项目复盘\n\n从接口到数据库的一致性。",
  );
  await page.locator("#snapshot").click();
  await synced(page);
  await page
    .locator("#image-file")
    .setInputFiles({
      name: "test.png",
      mimeType: "image/png",
      buffer: Buffer.from(
        "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aP1sAAAAASUVORK5CYII=",
        "base64",
      ),
    });
  await page.waitForFunction(() =>
    document.querySelector(".cm-content")?.textContent.includes("/assets/"),
  );
  await page.locator("#snapshot").click();
  await synced(page);
  let d = (await request(`/admin/articles/${id}`)).data;
  const asset = d.draft.body.match(/\/api\/v1\/articles\/[^)]+/)[0];
  assert.equal((await fetch(base + asset)).status, 404);
  await page.locator("#publish").click();
  await page.locator("#publish-dialog").waitFor({ state: "visible" });
  assert.ok(
    !(await page.locator("#publish-dialog").innerText()).includes(
      "PRIVATE COMPANY",
    ),
  );
  await page.locator("#confirm-publish").click();
  await synced(page);
  assert.equal((await fetch(base + asset)).status, 200);
  let pub = await request(`/articles/${id}`, "GET", null, null);
  assert.equal(pub.data.title, "Go 并发面试复盘");
  assert.ok(!JSON.stringify(pub).includes("PRIVATE"));
  await page.screenshot({
    path: "work/screenshots/editor-desktop.png",
    fullPage: true,
  });
  await page.locator("#title").fill("未发布的新标题");
  await page.locator("#snapshot").click();
  await synced(page);
  assert.equal(
    (await request(`/articles/${id}`)).data.title,
    "Go 并发面试复盘",
  );
  await context.setOffline(true);
  await editBody(page, "离线写下的重要面试回答，关闭后必须恢复。");
  await page.waitForFunction(() =>
    document
      .querySelector("#save-status")
      ?.textContent.includes("Saved locally"),
  );
  page.on("dialog", (dialog) => dialog.accept());
  await page.close();
  await context.setOffline(false);
  const recovered = await context.newPage();
  await recovered.goto(base + `/studio/articles/${id}/edit`);
  await recovered.locator(".cm-content").waitFor();
  assert.ok(
    (await recovered.locator(".cm-content").innerText()).includes(
      "关闭后必须恢复",
    ),
  );
  await synced(recovered);
  d = (await request(`/admin/articles/${id}`)).data;
  assert.ok(d.draft.body.includes("关闭后必须恢复"));
  const second = await context.newPage();
  await second.goto(base + `/studio/articles/${id}/edit`);
  await second.waitForFunction(() =>
    document.querySelector("#notice")?.textContent.includes("another tab"),
  );
  await second.close();
  const remote = {
    ...d.draft,
    title: "另一台设备的标题",
    body: "服务器新内容",
    version: d.draft.version,
    request_id: crypto.randomUUID(),
    action: "save",
  };
  assert.equal(
    (await request(`/admin/articles/${id}`, "PUT", remote)).status,
    200,
  );
  await editBody(recovered, "本机发生冲突后的内容");
  await recovered.locator("#snapshot").click();
  await recovered.locator("#conflict-dialog").waitFor({ state: "visible" });
  assert.ok(
    (await recovered.locator("#remote-conflict").innerText()).includes(
      "服务器新内容",
    ),
  );
  await recovered.locator("#keep-local").click();
  await synced(recovered);
  assert.equal(
    (await request(`/admin/articles/${id}`)).data.draft.body,
    "本机发生冲突后的内容",
  );
  await recovered.locator("#history").click();
  await recovered.locator("#history-dialog").waitFor({ state: "visible" });
  const publishedButton = recovered
    .locator(".revision-item")
    .filter({ hasText: "Publish" })
    .first();
  await publishedButton.click();
  await recovered.locator("#restore-revision").click();
  await synced(recovered);
  assert.ok(
    (await recovered.locator(".cm-content").innerText()).includes(
      "如何避免数据竞争",
    ),
  );
  recovered.on("dialog", (dialog) => dialog.accept());
  await recovered.locator("#trash").click();
  await synced(recovered);
  assert.equal((await request(`/articles/${id}`)).status, 404);
  await recovered.locator("#untrash").click();
  await synced(recovered);
  assert.equal((await request(`/articles/${id}`)).status, 404);
  await recovered.locator("#publish").click();
  await recovered.locator("#confirm-publish").click();
  await synced(recovered);
  const reader = await context.newPage();
  await reader.goto(base + `/articles/${id}`);
  await reader.locator("#article-body h2").first().waitFor();
  assert.equal(await reader.locator("textarea").count(), 0);
  assert.equal(await reader.locator("#article-body img").count(), 1);
  assert.ok(
    await reader
      .locator("#article-body img")
      .evaluate((img) => img.complete && img.naturalWidth > 0),
  );
  await reader.screenshot({
    path: "work/screenshots/article-desktop.png",
    fullPage: true,
  });
  await reader.setViewportSize({ width: 390, height: 844 });
  await reader.screenshot({
    path: "work/screenshots/article-mobile.png",
    fullPage: true,
  });
  assert.ok(
    await reader.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  );
  await recovered.setViewportSize({ width: 390, height: 844 });
  await recovered.screenshot({
    path: "work/screenshots/editor-mobile.png",
    fullPage: true,
  });
  assert.ok(
    await recovered.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  );
  assert.deepEqual(errors, []);
  await context.close();
});

test("lost save response retries once; expired login preserves draft; public Markdown is sanitized", async () => {
  const { context, page } = await pageForOwner();
  const id = crypto.randomUUID();
  await page.goto(base + `/studio/articles/${id}/edit`);
  await page.locator(".cm-content").waitFor();
  await synced(page);
  let dropped = false;
  await page.route(`**/api/v1/admin/articles/${id}`, async (route) => {
    if (route.request().method() === "PUT" && !dropped) {
      dropped = true;
      await route.fetch();
      await route.abort();
    } else await route.continue();
  });
  await page.locator("#title").fill("幂等保存测试");
  await page.locator("#snapshot").click();
  await page.waitForFunction(() =>
    document.querySelector("#save-status").textContent.includes("Sync failed"),
  );
  const version = (await request(`/admin/articles/${id}`)).data.draft.version;
  await page.locator("#retry").click();
  await synced(page);
  assert.equal(
    (await request(`/admin/articles/${id}`)).data.draft.version,
    version,
  );
  await page
    .locator("#tags")
    .fill(Array.from({ length: 13 }, (_, i) => `tag${i}`).join(","));
  await page.locator("#snapshot").click();
  await page.waitForFunction(() =>
    document.querySelector("#save-status").textContent.includes("Save rejected"),
  );
  await page.locator("#tags").fill("valid-tag");
  await page.locator("#snapshot").click();
  await synced(page);
  assert.deepEqual((await request(`/admin/articles/${id}`)).data.draft.tags, [
    "valid-tag",
  ]);
  await page.evaluate(() => localStorage.setItem("token", "expired-token"));
  await editBody(page, "登录过期后继续记录");
  await page.locator("#snapshot").click();
  await page.locator("#login-panel").waitFor({ state: "visible" });
  assert.ok(page.url().includes("/edit"));
  assert.ok(
    (await page.locator(".cm-content").innerText()).includes("登录过期"),
  );
  await page
    .locator("#login-form input[name=email]")
    .fill("owner@example.test");
  await page.locator("#login-form input[name=password]").fill(password);
  await page.locator("#login-form button").click();
  await synced(page);
  await editBody(
    page,
    '## Safe\n\n<script>window.__xss=1</script>\n\n<img src=x onerror="window.__xss=1">\n\n[bad](javascript:alert(1))',
  );
  await page.locator("#publish").click();
  await page.locator("#confirm-publish").click();
  await synced(page);
  await page.goto(base + `/articles/${id}`);
  await page.locator("#article-body h2").waitFor();
  assert.equal(await page.evaluate(() => window.__xss), undefined);
  assert.equal(
    await page
      .locator(
        '#article-body script, #article-body [onerror], #article-body a[href^="javascript:"]',
      )
      .count(),
    0,
  );
  await context.close();
});

test("reader cannot enter studio; public list supports search and mobile layout", async () => {
  const context = await browser.newContext({
    viewport: { width: 390, height: 844 },
  });
  const page = await context.newPage();
  await page.goto(base + "/studio/articles");
  await page.locator("#login-panel").waitFor({ state: "visible" });
  await page.locator("input[name=email]").fill("reader@example.test");
  await page.locator("input[name=password]").fill(password);
  await page.locator("#login-form button").click();
  await page.waitForFunction(() =>
    document
      .querySelector("#notice")
      ?.textContent.includes("only the blog owner"),
  );
  assert.equal(await page.locator("#studio-content").isVisible(), false);
  await page.goto(base + "/articles");
  await page.locator(".article-row").first().waitFor();
  assert.equal(await page.locator("#category option").count(), 5);
  await page.locator("#category").selectOption("fundamentals");
  await page.locator(".empty").waitFor();
  await page.locator("#category").selectOption("interview");
  await page.locator(".article-row").first().waitFor();
  assert.equal(await page.locator(".article-row").count(), 1);
  await page.locator("#category").selectOption("");
  await page.locator(".article-row").first().waitFor();
  await page.locator("#keyword").fill("不存在的关键词xyz");
  await page.locator("#filters button").click();
  await page.locator(".empty").waitFor();
  await page.locator("#reset-filters").click();
  await page.locator(".article-row").first().waitFor();
  assert.equal(await page.locator("#keyword").inputValue(), "");
  assert.equal(await page.locator("#category").inputValue(), "");
  assert.equal(await page.locator("#tag").inputValue(), "");
  assert.equal(await page.locator("#article-list").getAttribute("aria-busy"), "false");
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  );
  await page.screenshot({
    path: "work/screenshots/list-mobile.png",
    fullPage: true,
  });
  await context.close();
});

test("homepage: responsive navigation, former role and scroll feedback", async () => {
  const context = await browser.newContext({ reducedMotion: "reduce" });
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  for (const width of [320, 390, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto(base + "/");
    await page.locator(".past-note").waitFor();
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `overflow at ${width}px`);
    assert.equal(await page.locator(".exp-badge.past").innerText(), "Former role");
    assert.equal(await page.locator("#articles h2").innerText(), "Articles & notes.");
    if (width <= 760) {
      await page.locator(".menu-toggle").click();
      assert.equal(await page.locator(".menu-toggle").getAttribute("aria-expanded"), "true");
      await page.keyboard.press("Escape");
      assert.equal(await page.locator(".menu-toggle").getAttribute("aria-expanded"), "false");
      await page.locator(".menu-toggle").click();
      await page.locator('#main-nav a[href="#experience"]').click();
      assert.equal(await page.locator(".menu-toggle").getAttribute("aria-expanded"), "false");
    }
    await page.locator(".exp-card-header").first().click();
    assert.equal(await page.locator(".exp-card").first().getAttribute("open"), "");
    await page.evaluate(() => scrollTo(0, document.body.scrollHeight));
    await page.waitForFunction(() => document.querySelector(".site-header").classList.contains("is-scrolled"));
    assert.notEqual(await page.locator(".scroll-progress").evaluate(el => getComputedStyle(el).transform), "matrix(0, 0, 0, 1, 0, 0)");
    if ([390, 1440].includes(width)) {
      await page.evaluate(() => {
        document.activeElement?.blur();
        document.querySelectorAll('.exp-card').forEach(card => card.open = false);
        scrollTo(0, 0);
      });
      await page.waitForFunction(() => scrollY === 0);
      await page.screenshot({ path: `work/screenshots/home-${width}.png`, fullPage: true });
    }
  }
  assert.deepEqual(errors, []);
  await context.close();
});

test("typing during a delayed acknowledgment remains dirty and is saved subsequently", async () => {
  const { context, page } = await pageForOwner();
  const id = crypto.randomUUID();
  await page.goto(base + `/studio/articles/${id}/edit`);
  await page.locator(".cm-content").waitFor();
  await synced(page);
  let accepted;
  const acceptedPromise = new Promise(resolve => { accepted = resolve; });
  let release;
  const releasePromise = new Promise(resolve => { release = resolve; });
  let delayed = false;
  await page.route(`**/api/v1/admin/articles/${id}`, async route => {
    if (route.request().method() !== "PUT" || delayed) return route.continue();
    delayed = true;
    const response = await route.fetch();
    accepted();
    await releasePromise;
    await route.fulfill({ response });
  });
  await editBody(page, "第一段已发送");
  await page.locator("#snapshot").click();
  await acceptedPromise;
  await editBody(page, "第一段已发送，第二段在保存期间继续输入");
  release();
  await waitFor(async () => (await request(`/admin/articles/${id}`)).data.draft.body.includes("第二段"), "new typing was lost");
  await synced(page);
  await context.close();
});

test("homepage article entry and recent articles render without navigation errors", async () => {
  const context = await browser.newContext();
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.goto(base + "/");
  await page.locator("#recent-articles article").first().waitFor();
  assert.equal(await page.locator('nav a[href="/articles"]').count(), 1);
  for (const width of [1440, 850, 390]) {
    await page.setViewportSize({ width, height: 950 });
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `homepage overflow at ${width}`);
  }
  assert.deepEqual(errors, []);
  await context.close();
});
