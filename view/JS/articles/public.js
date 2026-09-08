import {
  $,
  api,
  card,
  categories,
  date,
  escape,
  icons,
  notice,
  pager,
  renderMarkdown,
} from "./common.js";
icons();
if ($("#article-list")) {
  const params = new URLSearchParams(location.search);
  $("#keyword").value = params.get("keyword") || "";
  $("#category").value = params.get("category") || "";
  $("#tag").value = params.get("tag") || "";
  let sequence = 0;
  async function load(page = 1) {
    const request = ++sequence;
    const query = new URLSearchParams({
      page,
      keyword: $("#keyword").value,
      category: $("#category").value,
      tag: $("#tag").value,
    });
    history.replaceState(null, "", `/articles?${query}`);
    try {
      const result = await api(`/articles?${query}`);
      if (request !== sequence) return;
      $("#article-list").innerHTML =
        result.data.map((d) => card(d)).join("") ||
        '<p class="empty">暂无文章</p>';
      pager(page, result.total, 12, load);
      notice("");
      icons();
    } catch (error) {
      notice(error.message, true);
    }
  }
  $("#filters").onsubmit = (e) => {
    e.preventDefault();
    load();
  };
  $("#category").onchange = () => load();
  load(Math.max(1, Number(params.get("page")) || 1));
} else {
  try {
    const id = location.pathname.split("/")[2];
    const { data: d } = await api(`/articles/${id}`);
    document.title = `${d.title} | Durlim`;
    $("#article-title").textContent = d.title;
    $("#article-meta").textContent =
      `${categories[d.category] || "文章"} · ${date(d.published_at)}`;
    $("#article-summary").textContent = d.summary;
    $("#article-tags").innerHTML = (d.tags || [])
      .map(
        (t) =>
          `<a href="/articles?tag=${encodeURIComponent(t)}">${escape(t)}</a>`,
      )
      .join("");
    await renderMarkdown($("#article-body"), d.body);
    const headings = [...$("#article-body").querySelectorAll("h1,h2,h3")];
    $("#toc").innerHTML = headings
      .map((h, i) => {
        h.id = `section-${i}`;
        return `<a class="toc-${h.tagName.toLowerCase()}" href="#${h.id}">${escape(h.textContent)}</a>`;
      })
      .join("");
    $("#toc-shell").hidden = headings.length === 0;
  } catch (error) {
    $("#article-title").textContent = "文章不可用";
    notice(
      error.status === 404 ? "文章未发布、已下架或不存在。" : error.message,
      true,
    );
  }
}
