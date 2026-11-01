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
    status("Local save failed. Export a backup now.", true);
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
  $("#word-count").textContent = `${content.body.length} characters`;
}
function changed() {
  if (muted || !writable || !record) return;
  record.content = capture();
  record.dirty = true;
  status("Saving locally…");
  $("#word-count").textContent = `${record.content.body.length} characters`;
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
    if (record.dirty) status("Saved locally, waiting to sync");
  } catch {
    /* Status already reports the failed local write. */
  }
}
function flags(d) {
  remoteDocument = d;
  $("#publish-state").textContent = d.trashed
    ? "Trash"
    : d.published_revision_id
      ? "Published"
      : "Draft";
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
  status("Syncing…");
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
        ? "Saved locally, waiting to sync"
        : `Synced with server · ${new Date().toLocaleTimeString("en-US")}`,
    );
    notice("");
    return true;
  } catch (error) {
    if (error.localSave) {
      status("Local save failed. Export a full backup now.", true);
    } else if (error.status === 400 || error.status === 413) {
      record.pending = null;
      record.dirty = true;
      await saveLocal();
      status("Save rejected. Check the title, tags and content length.", true);
    } else if (error.status === 409) {
      conflicted = true;
      await showConflict();
    } else if (error.status === 401 || error.status === 403) {
      authError(error);
      status("Local draft preserved. Please sign in again.", true);
    } else {
      status(
        navigator.onLine ? "Sync failed. Local draft preserved." : "Offline. Local draft preserved.",
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
    status("Version conflict. Both drafts have been preserved.", true);
  } catch (error) {
    notice("Cannot load the server version. Reconnect and retry.", true);
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
  else status("Server version loaded. Previous local draft backed up.");
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
      notice("Server unavailable. Local draft restored.", true);
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
    status("Local draft restored");
    if (!conflicted) await sync();
  } else status("Synced with server");
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
    if (ownerID !== owner) throw new Error("Sign in with the original draft owner account.");
    $("#login-panel").hidden = true;
    await sync();
    return;
  }
  if (!navigator.locks)
    throw new Error(
      "Safe multi-tab editing is unavailable. Use a recent Chrome, Edge or Firefox browser.",
    );
  navigator.locks.request(
    `article:${ownerID}:${id}`,
    { ifAvailable: true },
    async (lock) => {
      if (!lock) {
        notice("This article is open in another tab. Close that tab and refresh.", true);
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
  confirm("Unpublish this article? Drafts and revision history will be preserved.") && sync("unpublish");
$("#trash").onclick = () =>
  confirm("Move this article to trash and remove public access?") && sync("trash");
$("#untrash").onclick = () => sync("untrash");
async function revisions(page = 1) {
  revisionPage = page;
  const result = await api(`/admin/articles/${id}/revisions?page=${page}`);
  $("#revision-list").replaceChildren();
  for (const r of result.data) {
    const button = document.createElement("button");
    button.className = "revision-item";
    button.textContent = `v${r.version} · ${{ auto: "Auto snapshot", snapshot: "Manual save", publish: "Publish", restore: "Restore", before_restore: "Backup before restore", unpublish: "Unpublish", trash: "Move to trash", untrash: "Restore draft" }[r.reason] || r.reason} · ${date(r.created_at)}`;
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
  if (record.content.body && !confirm("Append an interview template to this draft?")) return;
  $("#category").value = "interview";
  view.dispatch({
    changes: {
      from: view.state.doc.length,
      insert:
        "\n## Interview overview\n\nDate:\nRole:\nRound:\n\n## Questions & answers\n\n### Question one\n\nMy answer:\n\nFollow-up:\n\n## Project discussion\n\n## Interview retrospective\n\n## Knowledge gaps\n\n- [ ] \n",
    },
  });
  changed();
};
$("#upload-image").onclick = () => $("#image-file").click();
$("#image-file").onchange = async (e) => {
  const file = e.target.files[0];
  if (!file) return;
  try {
    if (file.size > 5 * 1024 * 1024) throw new Error("Images must be 5 MB or smaller");
    if (!(await sync())) return;
    const form = new FormData();
    form.append("file", file);
    const { data } = await api(`/admin/articles/${id}/assets`, {
      method: "POST",
      body: form,
    });
    insert(`\n![Image](${data.url})\n`);
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
    if (file.size > 4 * 1024 * 1024) throw new Error("Backup file is too large");
    const data = JSON.parse(await file.text());
    const c = data.content;
    const valid =
      c &&
      ["title", "summary", "body", "category", "private_notes"].every(
        (k) => typeof c[k] === "string",
      ) &&
      Array.isArray(c.tags) &&
      c.tags.every((t) => typeof t === "string");
    if (!valid) throw new Error("Invalid article backup");
    if (!confirm("Replace this draft with the backup? A local copy of the current draft will be preserved."))
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
