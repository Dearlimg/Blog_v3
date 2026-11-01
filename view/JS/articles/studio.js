import {
  $,
  access,
  api,
  authError,
  card,
  date,
  download,
  escape,
  icons,
  notice,
  pager,
  setupLogin,
} from "./common.js";
import { allLocal } from "./storage.js";
let owner;
let state = "";
async function load(page = 1) {
  try {
    const result = await api(`/admin/articles?page=${page}&state=${state}`);
    $("#article-list").innerHTML =
      result.data.map((d) => card(d, true)).join("") ||
      '<p class="empty">No articles found.</p>';
    pager(page, result.total, 12, load);
    icons();
  } catch (error) {
    if (error.status === 401 || error.status === 403) authError(error);
    else notice(error.message, true);
  }
}
async function start() {
  owner = await access();
  $("#login-panel").hidden = true;
  $("#studio-content").hidden = false;
  notice("");
  await load();
  try {
    const records = (await allLocal()).filter(
      (r) => r.owner === owner && (r.dirty || r.pending || r.backup),
    );
    $("#local-list").replaceChildren();
    $("#local-section").hidden = !records.length;
    for (const r of records) {
      const row = document.createElement("div");
      row.className = "local-row";
      row.innerHTML = `<a href="/studio/articles/${r.id}/edit">${escape(r.content.title || "Untitled draft")}</a><span>${r.backup ? "Conflict backup" : "Local changes pending"} · ${escape(date(r.updatedAt))}</span>`;
      const button = document.createElement("button");
      button.className = "icon-button";
      button.title = "Export local backup";
      button.setAttribute("aria-label", "Export local backup");
      button.innerHTML = '<i data-lucide="download"></i>';
      button.onclick = () =>
        download(
          `${r.id}-backup.json`,
          JSON.stringify(r, null, 2),
          "application/json",
        );
      row.append(button);
      $("#local-list").append(row);
    }
    icons();
  } catch {
    notice("Local storage is unavailable. Check your browser settings.", true);
  }
}
setupLogin(start);
$("#new-article").onclick = () =>
  (location.href = `/studio/articles/${crypto.randomUUID()}/edit`);
$("#state-tabs").onclick = (e) => {
  const b = e.target.closest("[data-state]");
  if (!b) return;
  state = b.dataset.state;
  $("#state-tabs")
    .querySelectorAll("button")
    .forEach((el) => el.setAttribute("aria-pressed", String(el === b)));
  load();
};
$("#logout").onclick = () => {
  localStorage.removeItem("token");
  localStorage.removeItem("user");
  location.reload();
};
icons();
start().catch(authError);
