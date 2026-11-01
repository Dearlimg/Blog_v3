const target = document.querySelector("#recent-articles");
if (target) {
  try {
    const response = await fetch("/api/v1/articles");
    const result = await response.json();
    if (!response.ok) throw new Error("unavailable");
    target.replaceChildren();
    for (const article of result.data.slice(0, 3)) {
      const row = document.createElement("article");
      row.className = "recent-article";
      const title = document.createElement("h3");
      const link = document.createElement("a");
      link.href = `/articles/${encodeURIComponent(article.id)}`;
      link.textContent = article.title;
      title.append(link);
      const summary = document.createElement("p");
      summary.textContent = article.summary;
      row.append(title, summary);
      target.append(row);
    }
    if (!result.data.length) target.textContent = "No published articles yet.";
  } catch {
    target.textContent = "Articles are temporarily unavailable.";
  } finally {
    target.setAttribute("aria-busy", "false");
  }
}
