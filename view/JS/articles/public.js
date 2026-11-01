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
    const list = $("#article-list");
    list.setAttribute("aria-busy", "true");
    $("#previous").disabled = true;
    $("#next").disabled = true;
    notice("Loading articles…");
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
        '<p class="empty">No articles found.</p>';
      pager(page, result.total, 12, load);
      notice(`${result.total} article${result.total === 1 ? "" : "s"} found`);
      icons();
    } catch (error) {
      if (request !== sequence) return;
      list.innerHTML = '<p class="empty">Could not load articles. Try searching again.</p>';
      notice(error.message, true);
    } finally {
      if (request === sequence) list.setAttribute("aria-busy", "false");
    }
  }
  $("#filters").onsubmit = (e) => {
    e.preventDefault();
    load();
  };
  $("#category").onchange = () => load();
  $("#reset-filters").onclick = () => {
    $("#filters").reset();
    load();
    $("#keyword").focus();
  };
  load(Math.max(1, Number(params.get("page")) || 1));
} else {
  try {
    const id = location.pathname.split("/")[2];
    const { data: d } = await api(`/articles/${id}`);
    document.title = `${d.title} | Durlim`;
    $("#article-title").textContent = d.title;
    $("#article-meta").textContent =
      `${categories[d.category] || "Articles"} · ${date(d.published_at)}`;
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
    const tocLinks = [...$("#toc").querySelectorAll("a")];
    const observer = new IntersectionObserver((entries) => {
      const active = entries.find((entry) => entry.isIntersecting);
      if (!active) return;
      tocLinks.forEach((link) => {
        if (link.hash === `#${active.target.id}`) link.setAttribute("aria-current", "location");
        else link.removeAttribute("aria-current");
      });
    }, { rootMargin: "-100px 0px -55% 0px" });
    headings.forEach((heading) => observer.observe(heading));
    const progress = document.createElement("div");
    progress.className = "scroll-progress";
    progress.setAttribute("aria-hidden", "true");
    document.body.prepend(progress);
    function updateProgress() {
      const body = $("#article-body");
      const start = body.getBoundingClientRect().top + scrollY;
      const distance = body.offsetHeight - innerHeight;
      const ratio = distance > 0 ? (scrollY - start) / distance : (scrollY + innerHeight >= start + body.offsetHeight ? 1 : 0);
      progress.style.transform = `scaleX(${Math.max(0, Math.min(1, ratio))})`;
    }
    addEventListener("scroll", updateProgress, { passive: true });
    addEventListener("resize", updateProgress);
    updateProgress();
  } catch (error) {
    $("#article-title").textContent = "Article unavailable";
    notice(
      error.status === 404 ? "This article is unpublished or unavailable." : error.message,
      true,
    );
  }
}
