import { marked } from "marked";
import DOMPurify from "dompurify";
import hljs from "highlight.js/lib/common";
import {
  createIcons,
  ArrowLeft,
  ArrowRight,
  Plus,
  Save,
  Eye,
  Pencil,
  History,
  Upload,
  Download,
  Image,
  Bold,
  Italic,
  Code,
  List,
  Heading,
  X,
  RotateCcw,
  Trash2,
  LogOut,
  Search,
  Copy,
  ExternalLink,
} from "lucide";

export const categories = {
  fundamentals: "Fundamentals",
  project: "Project retrospectives",
  notes: "Learning notes",
  interview: "Interview notes",
};
export const $ = (selector) => document.querySelector(selector);
export function icons() {
  createIcons({
    icons: {
      ArrowLeft,
      ArrowRight,
      Plus,
      Save,
      Eye,
      Pencil,
      History,
      Upload,
      Download,
      Image,
      Bold,
      Italic,
      Code,
      List,
      Heading,
      X,
      RotateCcw,
      Trash2,
      LogOut,
      Search,
      Copy,
      ExternalLink,
    },
  });
}
export function escape(value = "") {
  return String(value).replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
}
export function date(value) {
  return value
    ? new Date(value).toLocaleString("en-US", { hour12: false })
    : "";
}
export function notice(message, error = false) {
  const el = $("#notice");
  if (!el) return;
  el.textContent = message;
  el.classList.toggle("error", error);
  el.hidden = !message;
}
export async function api(path, options = {}) {
  const headers = new Headers(options.headers);
  const token = localStorage.getItem("token");
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (options.body && !(options.body instanceof FormData))
    headers.set("Content-Type", "application/json");
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 15000);
  try {
    const response = await fetch(`/api/v1${path}`, {
      ...options,
      headers,
      signal: controller.signal,
      cache: "no-store",
    });
    const result = await response.json();
    if (!response.ok || result.code !== 0) {
      const error = new Error(result.msg || "Request failed");
      error.status = response.status;
      throw error;
    }
    return result;
  } finally {
    clearTimeout(timer);
  }
}
export async function access() {
  return (await api("/admin/articles/access")).data.owner_id;
}
export function authError(error) {
  notice(
    error.status === 403
      ? "This account cannot write articles. Sign in as the blog owner."
      : "Sign in as the blog owner to continue.",
    true,
  );
  $("#login-panel").hidden = false;
}
export function setupLogin(onSuccess) {
  $("#login-form")?.addEventListener("submit", async (e) => {
    e.preventDefault();
    const form = e.currentTarget;
    const button = form.querySelector("button");
    button.disabled = true;
    try {
      const result = await api("/users/login", {
        method: "POST",
        body: JSON.stringify({
          email: form.email.value,
          password: form.password.value,
        }),
      });
      localStorage.setItem("token", result.data.token);
      localStorage.setItem("user", JSON.stringify(result.data.user));
      form.password.value = "";
      await onSuccess();
    } catch (error) {
      notice(error.message, true);
    } finally {
      button.disabled = false;
    }
  });
}
export async function renderMarkdown(target, body, privateAssets = false) {
  target.innerHTML = DOMPurify.sanitize(marked.parse(body || ""), {
    FORBID_TAGS: ["style", "iframe", "form", "input", "button"],
    FORBID_ATTR: ["style"],
  });
  target.querySelectorAll("a").forEach((a) => {
    a.rel = "noopener noreferrer";
    if (a.origin !== location.origin) a.target = "_blank";
  });
  for (const img of target.querySelectorAll("img")) {
    const src = img.getAttribute("src") || "";
    if (
      !/^\/api\/v1\/articles\/[0-9a-f-]{36}\/assets\/[0-9a-f-]{36}$/.test(src)
    ) {
      img.remove();
      continue;
    }
    img.loading = "lazy";
    if (privateAssets) {
      img.removeAttribute("src");
      try {
        const res = await fetch(src.replace("/articles/", "/admin/articles/"), {
          headers: {
            Authorization: `Bearer ${localStorage.getItem("token") || ""}`,
          },
          cache: "no-store",
        });
        if (!res.ok) {
          img.alt = "Image unavailable";
          continue;
        }
        const url = URL.createObjectURL(await res.blob());
        img.onload = () => URL.revokeObjectURL(url);
        img.src = url;
      } catch {
        img.alt = "Image unavailable";
      }
    }
  }
  target.querySelectorAll("pre code").forEach((block) => {
    hljs.highlightElement(block);
    const copy = document.createElement("button");
    copy.className = "copy-code icon-button";
    copy.title = "Copy code";
    copy.setAttribute("aria-label", "Copy code");
    copy.innerHTML = '<i data-lucide="copy"></i>';
    copy.onclick = async () => {
      try {
        await navigator.clipboard.writeText(block.textContent);
        notice("Code copied");
      } catch {
        notice("Could not copy code", true);
      }
    };
    block.parentElement.append(copy);
  });
  icons();
}
export function download(name, content, type = "text/markdown;charset=utf-8") {
  const url = URL.createObjectURL(new Blob([content], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
export function card(d, admin = false) {
  const content = admin ? d.draft : d;
  const href = admin ? `/studio/articles/${d.id}/edit` : `/articles/${d.id}`;
  return `<article class="article-row"><div class="row-meta">${escape(categories[content.category] || "Uncategorized")}<br><time>${escape(date(content.updated_at || d.published_at))}</time></div><div><h2><a href="${href}">${escape(content.title || "Untitled article")}</a></h2><p>${escape(content.summary)}</p><div class="tags">${(content.tags || []).map((t) => `<a href="/articles?tag=${encodeURIComponent(t)}">${escape(t)}</a>`).join("")}</div></div><span class="row-state">${admin ? (d.trashed ? "Trash" : d.published_revision_id ? "Published" : "Draft") : '<i data-lucide="arrow-right"></i>'}</span></article>`;
}
export function pager(page, total, size, callback) {
  $("#page-label").textContent =
    `${page} / ${Math.max(1, Math.ceil(total / size))}`;
  $("#previous").disabled = page <= 1;
  $("#next").disabled = page * size >= total;
  $("#previous").onclick = () => callback(page - 1);
  $("#next").onclick = () => callback(page + 1);
}
