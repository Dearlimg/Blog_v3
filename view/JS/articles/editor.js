import { EditorView, basicSetup } from "codemirror";
import { markdown } from "@codemirror/lang-markdown";
import {
  $,
  access,
  api,
  authError,
  date,
  download,
  icons,
  notice,
  renderMarkdown,
  setupLogin,
} from "./common.js";
import { readLocal, writeLocal } from "./storage.js";

const id = location.pathname.split("/")[3];
const blank = {
  title: "",
  summary: "",
  body: "",
  category: "notes",
  tags: [],
  private_notes: "",
};
let record,
  owner,
  view,
  muted = false,
  syncing = false,
  conflicted = false,
  writable = false,
  remoteDocument;
let localTimer,
  remoteTimer,
  localMaxTimer,
  remoteMaxTimer,
  revisionPage = 1;
let persistence = Promise.resolve();
const equal = (a, b) => JSON.stringify(a) === JSON.stringify(b);
function status(text, error = false) {
  $("#save-status").textContent = text;
  $("#save-status").classList.toggle("error", error);
}
function persist() {
  if (!record) return Promise.resolve();
  record.updatedAt = new Date().toISOString();
  const copy = structuredClone(record);
  persistence = persistence.catch(() => {}).then(() => writeLocal(copy));
  return persistence.catch((error) => {
    error.localSave = true;
    status("本地保存失败，请立即导出备份", true);
    throw error;
  });
}
function capture() {
  return {
    title: $("#title").value,
    summary: $("#summary").value,
    body: view.state.doc.toString(),
    category: $("#category").value,
    tags: $("#tags")
      .value.split(/[,，]/)
      .map((t) => t.trim())
      .filter(Boolean),
    private_notes: $("#private-notes").value,
  };
}
function fill(content) {
  muted = true;
  $("#title").value = content.title;
  $("#summary").value = content.summary;
  $("#category").value = content.category || "notes";
  $("#tags").value = (content.tags || []).join(", ");
  $("#private-notes").value = content.private_notes || "";
  view.dispatch({
    changes: { from: 0, to: view.state.doc.length, insert: content.body },
  });
  muted = false;
  $("#word-count").textContent = `${content.body.length} 字符`;
}
function changed() {
  if (muted || !writable || !record) return;
  record.content = capture();
  record.dirty = true;
  status("正在保存到本机…");
  $("#word-count").textContent = `${record.content.body.length} 字符`;
  clearTimeout(localTimer);
  localTimer = setTimeout(saveLocal, 300);
  if (!localMaxTimer) localMaxTimer = setTimeout(saveLocal, 1000);
  clearTimeout(remoteTimer);
  remoteTimer = setTimeout(() => sync(), 2000);
  if (!remoteMaxTimer) remoteMaxTimer = setTimeout(() => sync(), 10000);
}
async function saveLocal() {
  clearTimeout(localTimer);
  clearTimeout(localMaxTimer);
  localMaxTimer = null;
  try {
    await persist();
    if (record.dirty) status("已保存到本机，等待同步");
  } catch {
    /* Status already reports the failed local write. */
  }
}
function flags(d) {
  remoteDocument = d;
  $("#publish-state").textContent = d.trashed
    ? "回收站"
    : d.published_revision_id
      ? "已发布"
      : "草稿";
  $("#unpublish").hidden = !d.published_revision_id || d.trashed;
  $("#untrash").hidden = !d.trashed;
  $("#trash").hidden = d.trashed;
  $("#publish").disabled = d.trashed;
  $("#snapshot").disabled = d.trashed;
  $("#published-link").hidden = !d.published_revision_id || d.trashed;
  $("#published-link").href = `/articles/${id}`;
  $("#editing-fields").inert = d.trashed;
}
async function sendPending() {
  const pending = record.pending;
  if (!pending) return;
  if (pending.action === "restore") $("#editing-fields").inert = true;
  const { data: d } = await api(`/admin/articles/${id}`, {
    method: "PUT",
    body: JSON.stringify(pending),
  });
  record.version = d.draft.version;
  record.pending = null;
  if (pending.action === "restore") {
    record.content = { ...blank, ...d.draft };
    delete record.content.article_id;
    delete record.content.version;
    delete record.content.updated_at;
    fill(record.content);
  }
  record.dirty = !equal(record.content, {
    title: pending.title,
    summary: pending.summary,
    body: pending.body,
    category: pending.category,
    tags: pending.tags,
    private_notes: pending.private_notes,
  });
  if (pending.action === "restore") record.dirty = false;
  flags(d);
  await persist();
}
async function sync(action = "save", revisionID = 0) {
  clearTimeout(remoteTimer);
  clearTimeout(remoteMaxTimer);
  remoteMaxTimer = null;
  if (!writable || syncing || conflicted) return false;
  if (action === "save" && !record.dirty && !record.pending) return true;
  syncing = true;
  status("正在同步…");
  try {
    await persist();
    await sendPending();
    if (record.dirty && !["save", "snapshot", "publish"].includes(action)) {
      record.pending = {
        ...structuredClone(record.content),
        version: record.version,
        request_id: crypto.randomUUID(),
        action: "save",
        revision_id: 0,
      };
      await persist();
      await sendPending();
    }
    if (record.dirty || action !== "save") {
      record.pending = {
        ...structuredClone(record.content),
        version: record.version,
        request_id: crypto.randomUUID(),
        action,
        revision_id: revisionID,
      };
      await persist();
      await sendPending();
    }
    status(
      record.dirty
        ? "已保存到本机，等待同步"
        : `已同步服务器 · ${new Date().toLocaleTimeString("zh-CN")}`,
    );
    notice("");
    return true;
  } catch (error) {
    if (error.localSave) {
      status("本地保存失败，请立即导出完整备份", true);
    } else if (error.status === 400 || error.status === 413) {
      record.pending = null;
      record.dirty = true;
      await saveLocal();
      status("保存被拒绝，请检查标题、标签和内容长度", true);
    } else if (error.status === 409) {
      conflicted = true;
      await showConflict();
    } else if (error.status === 401 || error.status === 403) {
      authError(error);
      status("已保留本地草稿，请重新登录", true);
    } else {
      status(
        navigator.onLine ? "同步失败，本地草稿已保留" : "离线，本地草稿已保留",
        true,
      );
      notice(error.message, true);
    }
    return false;
  } finally {
    syncing = false;
    $("#editing-fields").inert = Boolean(remoteDocument?.trashed);
  }
}
async function showConflict() {
  try {
    const { data: d } = await api(`/admin/articles/${id}`);
    remoteDocument = d;
    $("#local-conflict").textContent = JSON.stringify(record.content, null, 2);
    $("#remote-conflict").textContent = JSON.stringify(
      contentOf(d.draft),
      null,
      2,
    );
    $("#keep-local").disabled = d.trashed;
    $("#conflict-dialog").showModal();
    status("版本冲突，两份内容均已保留", true);
  } catch (error) {
    notice("无法读取服务器版本，请联网后点击重试。", true);
  }
}
async function resolveConflict(keepLocal) {
  if (syncing) return;
  const backup = {
    ...structuredClone(record),
    key: `${record.key}:backup:${crypto.randomUUID()}`,
    backup: true,
  };
  await writeLocal(backup);
  const { data: d } = await api(`/admin/articles/${id}`);
  if (d.draft.version !== remoteDocument.draft.version) {
    remoteDocument = d;
    await showConflict();
    return;
  }
  record.version = d.draft.version;
  record.pending = null;
  record.dirty = keepLocal;
  if (!keepLocal) {
    record.content = contentOf(d.draft);
    fill(record.content);
  }
  flags(d);
  conflicted = false;
  await persist();
  $("#conflict-dialog").close();
  if (keepLocal) await sync();
  else status("已采用服务器版本，本地旧稿已备份");
}
function contentOf(draft) {
  return {
    title: draft.title,
    summary: draft.summary,
    body: draft.body,
    category: draft.category,
    tags: draft.tags || [],
    private_notes: draft.private_notes || "",
  };
}
async function initialize(ownerID) {
  owner = ownerID;
  const key = `${owner}:${id}`;
  record = await readLocal(key);
  let server;
  try {
    server = (await api(`/admin/articles/${id}`)).data;
  } catch (error) {
    if (error.status !== 404) {
      if (!record || error.status === 401 || error.status === 403) throw error;
      notice("暂时无法连接服务器，已恢复本地草稿。", true);
    }
  }
  if (!record)
    record = {
      key,
      id,
      owner,
      version: server?.draft.version || 0,
      content: server ? contentOf(server.draft) : structuredClone(blank),
      dirty: !server,
      pending: null,
    };
  writable = true;
  $("#editor-content").hidden = false;
  $("#login-panel").hidden = true;
  view = new EditorView({
    doc: record.content.body,
    extensions: [
      basicSetup,
      markdown(),
      EditorView.lineWrapping,
      EditorView.updateListener.of((update) => {
        if (update.docChanged) changed();
      }),
    ],
    parent: $("#code-editor"),
  });
  fill(record.content);
  if (server) flags(server);
  await persist();
  if (server && record.version !== server.draft.version && !record.pending) {
    if (record.dirty) {
      conflicted = true;
      await showConflict();
    } else {
      record.content = contentOf(server.draft);
      record.version = server.draft.version;
      fill(record.content);
      await persist();
    }
  }
  if (record.dirty || record.pending) {
    status("已恢复本地草稿");
    if (!conflicted) await sync();
  } else status("已同步服务器");
}
async function start() {
  let ownerID;
  try {
    ownerID = await access();
    localStorage.setItem("article-owner", String(ownerID));
  } catch (error) {
    const cached = Number(localStorage.getItem("article-owner"));
    if (error.status || !cached || !(await readLocal(`${cached}:${id}`)))
      throw error;
    ownerID = cached;
  }
  if (record) {
    if (ownerID !== owner) throw new Error("请登录原草稿所属的博主账号。");
    $("#login-panel").hidden = true;
    await sync();
    return;
  }
  if (!navigator.locks)
    throw new Error(
      "当前浏览器不支持安全的多标签页编辑，请使用新版 Chrome、Edge 或 Firefox。",
    );
  navigator.locks.request(
    `article:${ownerID}:${id}`,
    { ifAvailable: true },
    async (lock) => {
      if (!lock) {
        notice("此文章已在另一个标签页编辑，请关闭那个页面后刷新。", true);
        return;
      }
      try {
        await initialize(ownerID);
        await new Promise(() => {});
      } catch (error) {
        notice(error.message, true);
      }
    },
  );
}
setupLogin(start);
for (const name of ["title", "summary", "category", "tags", "private-notes"])
  $(`#${name}`).addEventListener("input", changed);
$("#snapshot").onclick = () => sync("snapshot");
$("#retry").onclick = () => (conflicted ? showConflict() : sync());
$("#export").onclick = () =>
  download(
    `${record.content.title || "article"}-backup.json`,
    JSON.stringify(record, null, 2),
    "application/json",
  );
$("#export-markdown").onclick = () =>
  download(
    `${record.content.title || "article"}.md`,
    `# ${record.content.title}\n\n${record.content.body}`,
  );
$("#keep-local").onclick = () =>
  resolveConflict(true).catch((e) => notice(e.message, true));
$("#keep-remote").onclick = () =>
  resolveConflict(false).catch((e) => notice(e.message, true));
$("#export-conflict").onclick = () =>
  download(
    `${id}-conflict.json`,
    JSON.stringify(record, null, 2),
    "application/json",
  );
$("#preview-button").onclick = async () => {
  await renderMarkdown($("#preview-body"), record.content.body, true);
  $("#preview-title").textContent = record.content.title;
  $("#preview-dialog").showModal();
};
$("#publish").onclick = async () => {
  if (!(await sync())) return;
  await renderMarkdown($("#publish-body"), record.content.body, true);
  $("#publish-title").textContent = record.content.title;
  $("#publish-summary").textContent = record.content.summary;
  $("#publish-dialog").showModal();
};
$("#confirm-publish").onclick = async () => {
  if (await sync("publish")) $("#publish-dialog").close();
};
$("#unpublish").onclick = () =>
  confirm("下架后访客将无法阅读，草稿和历史版本保留。") && sync("unpublish");
$("#trash").onclick = () =>
  confirm("将文章移入回收站并停止公开展示？") && sync("trash");
$("#untrash").onclick = () => sync("untrash");
async function revisions(page = 1) {
  revisionPage = page;
  const result = await api(`/admin/articles/${id}/revisions?page=${page}`);
  $("#revision-list").replaceChildren();
  for (const r of result.data) {
    const button = document.createElement("button");
    button.className = "revision-item";
    button.textContent = `v${r.version} · ${{ auto: "自动快照", snapshot: "手动保存", publish: "发布", restore: "恢复", before_restore: "恢复前备份", unpublish: "下架", trash: "移入回收站", untrash: "恢复草稿" }[r.reason] || r.reason} · ${date(r.created_at)}`;
    button.onclick = async () => {
      try {
        const { data: full } = await api(
          `/admin/articles/${id}/revisions/${r.id}`,
        );
        $("#revision-title").textContent = full.title;
        await renderMarkdown($("#revision-body"), full.body, true);
        $("#revision-private").textContent = full.private_notes || "";
        $("#restore-revision").hidden = false;
        $("#restore-revision").onclick = async () => {
          if (await sync("restore", r.id)) $("#history-dialog").close();
        };
      } catch (error) {
        notice(error.message, true);
      }
    };
    $("#revision-list").append(button);
  }
  $("#history-previous").disabled = page === 1;
  $("#history-next").disabled = page * 20 >= result.total;
  $("#history-page").textContent =
    `${page} / ${Math.max(1, Math.ceil(result.total / 20))}`;
}
$("#history").onclick = async () => {
  try {
    if (!(await sync())) return;
    await revisions();
    $("#history-dialog").showModal();
  } catch (e) {
    notice(e.message, true);
  }
};
$("#history-previous").onclick = () =>
  revisions(revisionPage - 1).catch((e) => notice(e.message, true));
$("#history-next").onclick = () =>
  revisions(revisionPage + 1).catch((e) => notice(e.message, true));
document
  .querySelectorAll("[data-close]")
  .forEach((b) => (b.onclick = () => b.closest("dialog").close()));
function insert(before, after = "") {
  const range = view.state.selection.main;
  const selected = view.state.sliceDoc(range.from, range.to);
  view.dispatch({
    changes: {
      from: range.from,
      to: range.to,
      insert: before + selected + after,
    },
    selection: {
      anchor: range.from + before.length,
      head: range.from + before.length + selected.length,
    },
  });
  view.focus();
}
$("#format-tools").onclick = (e) => {
  const button = e.target.closest("[data-format]");
  if (!button) return;
  const formats = {
    bold: ["**", "**"],
    italic: ["*", "*"],
    heading: ["\n## ", ""],
    list: ["\n- ", ""],
    code: ["\n```go\n", "\n```\n"],
  };
  insert(...formats[button.dataset.format]);
};
$("#template").onclick = () => {
  if (record.content.body && !confirm("在当前正文末尾追加面试模板？")) return;
  $("#category").value = "interview";
  view.dispatch({
    changes: {
      from: view.state.doc.length,
      insert:
        "\n## 面试概况\n\n日期：\n岗位：\n轮次：\n\n## 问题与回答\n\n### 问题一\n\n我的回答：\n\n追问：\n\n## 项目讨论\n\n## 面试复盘\n\n## 待补知识\n\n- [ ] \n",
    },
  });
  changed();
};
$("#upload-image").onclick = () => $("#image-file").click();
$("#image-file").onchange = async (e) => {
  const file = e.target.files[0];
  if (!file) return;
  try {
    if (file.size > 5 * 1024 * 1024) throw new Error("图片不能超过 5 MB");
    if (!(await sync())) return;
    const form = new FormData();
    form.append("file", file);
    const { data } = await api(`/admin/articles/${id}/assets`, {
      method: "POST",
      body: form,
    });
    insert(`\n![图片](${data.url})\n`);
  } catch (error) {
    notice(error.message, true);
  } finally {
    e.target.value = "";
  }
};
$("#import-backup").onclick = () => $("#backup-file").click();
$("#backup-file").onchange = async (e) => {
  const file = e.target.files[0];
  if (!file) return;
  try {
    if (file.size > 4 * 1024 * 1024) throw new Error("备份文件过大");
    const data = JSON.parse(await file.text());
    const c = data.content;
    const valid =
      c &&
      ["title", "summary", "body", "category", "private_notes"].every(
        (k) => typeof c[k] === "string",
      ) &&
      Array.isArray(c.tags) &&
      c.tags.every((t) => typeof t === "string");
    if (!valid) throw new Error("无效的文章备份");
    if (!confirm("用备份内容替换当前草稿？当前内容会保留一份本地备份。"))
      return;
    await writeLocal({
      ...structuredClone(record),
      key: `${record.key}:backup:${crypto.randomUUID()}`,
      backup: true,
    });
    fill(contentOf(c));
    changed();
  } catch (error) {
    notice(error.message, true);
  } finally {
    e.target.value = "";
  }
};
window.addEventListener("online", () => sync());
window.addEventListener("beforeunload", (e) => {
  if (record && (record.dirty || record.pending)) {
    saveLocal();
    e.preventDefault();
    e.returnValue = "";
  }
});
document.addEventListener("visibilitychange", () => {
  if (document.hidden && record) {
    saveLocal();
    sync();
  }
});
setInterval(() => {
  if (record && !conflicted && (record.dirty || record.pending)) sync();
}, 15000);
document.addEventListener("keydown", (e) => {
  if ((e.ctrlKey || e.metaKey) && e.key === "s") {
    e.preventDefault();
    sync("snapshot");
  }
});
icons();
start().catch((error) => {
  if (error.status === 401 || error.status === 403) authError(error);
  else notice(error.message, true);
});
