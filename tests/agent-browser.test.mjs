import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { chromium } from "playwright";

test("interrupted approvals show unknown outcome without replay controls", async () => {
  const browser = await chromium.launch({
    headless: true,
    ...(process.env.PLAYWRIGHT_CHANNEL
      ? { channel: process.env.PLAYWRIGHT_CHANNEL }
      : {}),
  });
  try {
    const page = await browser.newPage();
    const errors = [];
    page.on("pageerror", error => errors.push(error.message));
    await page.addInitScript(() => localStorage.setItem(
      "durlim-agent-sessions",
      JSON.stringify([{ id: "session", title: "Recovered session" }]),
    ));
    const approvals = [
      { id: "uncertain", tool: "mcp_call", status: "interrupted", arguments: { action: "send" } },
      { id: "pending", tool: "memory_write", status: "pending", arguments: { content: "English" } },
    ];
    let mutationRequests = 0;
    await page.route("**/*", async route => {
      const path = new URL(route.request().url()).pathname;
      const json = data => route.fulfill({
        contentType: "application/json", body: JSON.stringify(data),
      });
      if (route.request().method() === "POST") {
        mutationRequests++;
        return route.abort();
      }
      if (path === "/api/v1/agent/info") return json({ configured: true, owner: false });
      if (path === "/api/v1/agent/sessions/session") return json({
        id: "session", messages: [], active_run: "", approvals, todos: [],
      });
      const files = {
        "/agent": ["view/html/agent.html", "text/html"],
        "/view/JS/agent.js": ["view/JS/agent.js", "text/javascript"],
        "/static/agent-build/agent.js": ["static/agent-build/agent.js", "text/javascript"],
        "/static/agent-build/agent.css": ["static/agent-build/agent.css", "text/css"],
        "/view/css/agent.css": ["view/css/agent.css", "text/css"],
      };
      if (files[path]) {
        const [file, contentType] = files[path];
        return route.fulfill({ contentType, body: await readFile(file, "utf8") });
      }
      return route.abort();
    });
    await page.goto("http://agent.test/agent");
    await page.locator("#sessions button").click();
    await page.locator(".approval").first().waitFor();
    const interrupted = page.locator(".approval").filter({ hasText: "操作结果待核实" });
    assert.equal(await interrupted.count(), 1);
    assert.match(await interrupted.innerText(), /系统不会自动重试/);
    assert.equal(await interrupted.locator("button").count(), 0);
    const pending = page.locator(".approval").filter({ hasText: "待确认操作" });
    assert.equal(await pending.locator("button").count(), 2);
    assert.equal(mutationRequests, 0);
    assert.deepEqual(errors, []);
  } finally {
    await browser.close();
  }
});

for (const delayed of [false, true]) {
  test(`owner login resets visitor run state (delayed start: ${delayed})`, async () => {
    const browser = await chromium.launch({
      headless: true,
      ...(process.env.PLAYWRIGHT_CHANNEL ? { channel: process.env.PLAYWRIGHT_CHANNEL } : {}),
    });
    try {
      const page = await browser.newPage();
      const errors = [];
      page.on("pageerror", error => errors.push(error.message));
      const cancellations = [];
      const wrongIdentity = [];
      let release;
      let started;
      const pending = new Promise(resolve => { started = resolve; });
      const gate = new Promise(resolve => { release = resolve; });
      await page.route("**/*", async route => {
        const path = new URL(route.request().url()).pathname;
        const auth = route.request().headers().authorization || "";
        const json = data => route.fulfill({
          contentType: "application/json", body: JSON.stringify(data),
        });
        if (path === "/api/v1/agent/info") return json({ configured: true, owner: auth === "Bearer valid" });
        if (path === "/api/v1/agent/owner/state") return json({ jobs: [] });
        if (path === "/api/v1/agent/sessions") return json({ id: "visitor" });
        if (path === "/api/v1/agent/sessions/visitor/messages") {
          started();
          if (delayed) await gate;
          return json({ run_id: "visitor-run" });
        }
        if (path.includes("visitor-run")) {
          if (auth) wrongIdentity.push(auth);
          if (path.endsWith("/cancel")) {
            cancellations.push(auth);
            return json({ status: "cancellation_requested" });
          }
          return json({ status: "running", events: [] });
        }
        const files = {
          "/agent": ["view/html/agent.html", "text/html"],
          "/view/JS/agent.js": ["view/JS/agent.js", "text/javascript"],
          "/static/agent-build/agent.js": ["static/agent-build/agent.js", "text/javascript"],
          "/static/agent-build/agent.css": ["static/agent-build/agent.css", "text/css"],
          "/view/css/agent.css": ["view/css/agent.css", "text/css"],
        };
        if (files[path]) {
          const [file, contentType] = files[path];
          return route.fulfill({ contentType, body: await readFile(file, "utf8") });
        }
        return route.abort();
      });
      await page.goto("http://agent.test/agent");
      await page.locator("#prompt").fill("Visitor question");
      await page.locator("#send").click();
      await pending;
      if (!delayed) await page.waitForFunction(() => document.querySelector("#run-status").textContent.includes("正在思考"));
      await page.locator("#owner-toggle").click();
      await page.locator("#owner-token").fill("valid");
      await page.locator("#owner-login button").click();
      await page.locator("#owner-tools").waitFor({ state: "visible" });
      release();
      await page.waitForFunction(() => !document.querySelector("#prompt").disabled);
      await page.waitForTimeout(1500);
      assert.equal(await page.locator("#new-chat").isDisabled(), false);
      assert.equal(await page.locator("#cancel").isVisible(), false);
      assert.equal(await page.locator(".message").count(), 0);
      assert.equal(cancellations.length, 1);
      assert.deepEqual(wrongIdentity, []);
      assert.deepEqual(errors, []);
    } finally {
      await browser.close();
    }
  });
}
