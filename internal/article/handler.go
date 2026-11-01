package article

import (
	"bytes"
	"crypto/rand"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"image"
	_ "image/gif" // Register the image formats accepted by uploads.
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"blog-front/internal/user"
	"blog-front/pkg/middleware"
	"blog-front/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Handler struct {
	svc     *Service
	siteURL string
}

type articlePage struct {
	Title        string
	Description  string
	CanonicalURL string
	ImageURL     string
	PublishedAt  string
	NoIndex      bool
}

type sitemapLocation struct {
	Location     string `xml:"loc"`
	LastModified string `xml:"lastmod,omitempty"`
}

type sitemapDocument struct {
	XMLName xml.Name          `xml:"urlset"`
	XMLNS   string            `xml:"xmlns,attr"`
	URLs    []sitemapLocation `xml:"url"`
}

func Register(
	r *gin.Engine,
	db *gorm.DB,
	secret string,
	owner uint,
	siteURLs ...string,
) {
	siteURL := ""
	if len(siteURLs) > 0 {
		siteURL = siteURLs[0]
	}
	h := &Handler{svc: NewService(db), siteURL: siteURL}
	r.GET("/articles", h.ArticleListPage)
	r.GET("/articles/:id", h.PublicPage)
	r.GET("/studio/articles", func(c *gin.Context) { c.File("./view/html/studio.html") })
	r.GET("/studio/articles/:id/edit", func(c *gin.Context) { c.File("./view/html/editor.html") })
	r.GET("/sitemap.xml", h.Sitemap)
	r.GET("/robots.txt", h.Robots)
	api := r.Group("/api/v1/articles")
	api.GET("", h.PublicList)
	api.GET("/:id", h.PublicGet)
	api.GET("/:id/assets/:asset", h.PublicAsset)
	admin := r.Group("/api/v1/admin/articles")
	admin.Use(middleware.Auth(secret), ownerOnly(db, owner))
	admin.GET("/access", func(c *gin.Context) { response.Success(c, gin.H{"owner_id": owner}) })
	admin.GET("", h.List)
	admin.GET("/:id", h.Get)
	admin.PUT("/:id", h.Mutate)
	admin.GET("/:id/revisions", h.Revisions)
	admin.GET("/:id/revisions/:revision", h.Revision)
	admin.POST("/:id/assets", h.Upload)
	admin.GET("/:id/assets/:asset", h.PrivateAsset)
}

func ownerOnly(db *gorm.DB, owner uint) gin.HandlerFunc {
	return func(c *gin.Context) {
		if owner == 0 || c.GetUint("user_id") != owner {
			response.Forbidden(c, "only the blog owner can write articles")
			c.Abort()
			return
		}
		var u user.Entity
		if err := db.First(&u, owner).Error; err != nil {
			response.Forbidden(c, "owner account unavailable")
			c.Abort()
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Next()
	}
}

func fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrConflict):
		response.Error(c, 409, 409, err.Error())
	case errors.Is(err, ErrInvalid):
		response.BadRequest(c, err.Error())
	case errors.Is(err, gorm.ErrRecordNotFound):
		response.NotFound(c, "article not found")
	default:
		response.InternalError(c, "article operation failed")
	}
}

func filter(c *gin.Context) Filter {
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		page = 100000
	}
	return Filter{Page: page, Category: c.Query("category"), Tag: c.Query("tag"),
		Keyword: c.Query("keyword"), State: c.Query("state")}
}

func (h *Handler) PublicList(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	f := filter(c)
	items, total, err := h.svc.repo.PublicList(f)
	if err != nil {
		fail(c, err)
		return
	}
	response.SuccessPage(c, items, total, f.Page, 12)
}
func (h *Handler) PublicGet(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	d, err := h.svc.repo.PublicGet(c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, d)
}
func (h *Handler) PublicPage(c *gin.Context) {
	document, err := h.svc.repo.PublicGet(c.Param("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = http.StatusNotFound
		}
		metadata := h.pageMetadata(c, nil)
		metadata.NoIndex = true
		h.renderPublicPage(c, metadata, status)
		return
	}

	h.renderPublicPage(c, h.pageMetadata(c, document), http.StatusOK)
}

func (h *Handler) ArticleListPage(c *gin.Context) {
	baseURL := h.baseURL(c)
	metadata := articlePage{
		Title:        "Articles | Durlim",
		Description:  "Articles and notes on backend development, systems and learning by Durlim.",
		CanonicalURL: baseURL + "/articles",
		ImageURL:     baseURL + "/static/Ava.jpg",
	}
	h.renderPublicPageFile(c, "./view/html/articles.html", metadata, http.StatusOK)
}

func (h *Handler) Sitemap(c *gin.Context) {
	entries, err := h.svc.repo.PublicSitemapEntries()
	if err != nil {
		slog.Error("generate sitemap failed", "error", err)
		c.String(http.StatusInternalServerError, "sitemap unavailable")
		return
	}

	baseURL := h.baseURL(c)
	locations := []sitemapLocation{
		{Location: baseURL + "/"},
		{Location: baseURL + "/articles"},
	}
	for _, entry := range entries {
		location := sitemapLocation{
			Location: baseURL + "/articles/" + url.PathEscape(entry.ID),
		}
		if entry.PublishedAt != nil {
			location.LastModified = entry.PublishedAt.UTC().Format(time.RFC3339)
		}
		locations = append(locations, location)
	}

	document := sitemapDocument{
		XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  locations,
	}
	body, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		slog.Error("encode sitemap failed", "error", err)
		c.String(http.StatusInternalServerError, "sitemap unavailable")
		return
	}

	c.Header("Cache-Control", "public, max-age=300")
	c.Data(
		http.StatusOK,
		"application/xml; charset=utf-8",
		append([]byte(xml.Header), body...),
	)
}

func (h *Handler) Robots(c *gin.Context) {
	baseURL := h.baseURL(c)
	content := fmt.Sprintf(
		"User-agent: *%cAllow: /%cDisallow: /studio/%cSitemap: %s/sitemap.xml%c",
		10,
		10,
		10,
		baseURL,
		10,
	)
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(content))
}

func (h *Handler) baseURL(c *gin.Context) string {
	if h.siteURL != "" {
		return strings.TrimRight(h.siteURL, "/")
	}

	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}

func (h *Handler) pageMetadata(c *gin.Context, document *PublicDocument) articlePage {
	baseURL := h.baseURL(c)
	page := articlePage{
		Title:        "Article unavailable | Durlim",
		Description:  "Articles and notes by Durlim.",
		CanonicalURL: baseURL + "/articles/" + url.PathEscape(c.Param("id")),
		ImageURL:     baseURL + "/static/Ava.jpg",
	}
	if document == nil {
		return page
	}

	title := strings.TrimSpace(document.Title)
	if title == "" {
		title = "Article"
	}
	description := strings.TrimSpace(document.Summary)
	if description == "" {
		description = title
	}
	page.Title = title + " | Durlim"
	page.Description = truncateDescription(description)
	if document.PublishedAt != nil {
		page.PublishedAt = document.PublishedAt.UTC().Format(time.RFC3339)
	}
	return page
}

func (h *Handler) renderPublicPage(c *gin.Context, metadata articlePage, status int) {
	h.renderPublicPageFile(c, "./view/html/article.html", metadata, status)
}

func (h *Handler) renderPublicPageFile(c *gin.Context, path string, metadata articlePage, status int) {
	page, err := template.ParseFiles(path)
	if err != nil {
		slog.Error("load public page template failed", "error", err)
		c.String(http.StatusInternalServerError, "page unavailable")
		return
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("X-Content-Type-Options", "nosniff")
	if status == http.StatusOK {
		c.Header("Cache-Control", "public, max-age=300")
	} else {
		c.Header("Cache-Control", "no-store")
	}
	c.Status(status)
	if err := page.ExecuteTemplate(c.Writer, strings.TrimPrefix(path, "./view/html/"), metadata); err != nil {
		slog.Error("render public page failed", "error", err)
	}
}

func truncateDescription(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= 160 {
		return value
	}
	return string(runes[:157]) + "..."
}

func (h *Handler) List(c *gin.Context) {
	f := filter(c)
	items, total, err := h.svc.repo.List(c.GetUint("user_id"), f)
	if err != nil {
		fail(c, err)
		return
	}
	response.SuccessPage(c, items, total, f.Page, 12)
}
func (h *Handler) Get(c *gin.Context) {
	d, err := h.svc.Get(c.GetUint("user_id"), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, d)
}
func (h *Handler) Mutate(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 3*1024*1024)
	var m Mutation
	if err := c.ShouldBindJSON(&m); err != nil {
		response.BadRequest(c, "invalid document")
		return
	}
	d, err := h.svc.Mutate(c.GetUint("user_id"), c.Param("id"), m)
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, d)
}
func (h *Handler) Revisions(c *gin.Context) {
	f := filter(c)
	items, total, err := h.svc.Revisions(c.GetUint("user_id"), c.Param("id"), f.Page)
	if err != nil {
		fail(c, err)
		return
	}
	response.SuccessPage(c, items, total, f.Page, 20)
}
func (h *Handler) Revision(c *gin.Context) {
	if _, err := h.svc.Get(c.GetUint("user_id"), c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	var r Revision
	err := h.svc.repo.db.First(&r, "id = ? AND article_id = ?", c.Param("revision"), c.Param("id")).Error
	if err != nil {
		fail(c, err)
		return
	}
	response.Success(c, r)
}

func (h *Handler) Upload(c *gin.Context) {
	d, err := h.svc.Get(c.GetUint("user_id"), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	if d.Trashed {
		fail(c, ErrConflict)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 5*1024*1024+65536)
	file, _, err := c.Request.FormFile("file")
	if c.Request.MultipartForm != nil {
		defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	}
	if err != nil {
		response.BadRequest(c, "image required (max 5 MB)")
		return
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, 5*1024*1024+1))
	if err != nil || len(data) > 5*1024*1024 {
		response.BadRequest(c, "image too large")
		return
	}
	info, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		response.BadRequest(c, "only PNG, JPEG and GIF images are supported")
		return
	}
	if info.Width > 10000 || info.Height > 10000 {
		response.BadRequest(c, "image dimensions too large")
		return
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		fail(c, err)
		return
	}
	id := fmt.Sprintf("%x-%x-%x-%x-%x", random[:4], random[4:6], random[6:8], random[8:10], random[10:])
	a := Asset{ID: id, ArticleID: d.ID, Data: data, MediaType: "image/" + format}
	if err := h.svc.repo.db.Create(&a).Error; err != nil {
		fail(c, err)
		return
	}
	response.Success(c, gin.H{"url": "/api/v1/articles/" + d.ID + "/assets/" + id})
}

func (h *Handler) PublicAsset(c *gin.Context) {
	d, err := h.svc.repo.PublicGet(c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	url := "/api/v1/articles/" + d.ID + "/assets/" + c.Param("asset")
	if !strings.Contains(d.Body, url) {
		response.NotFound(c, "image not found")
		return
	}
	h.asset(c)
}
func (h *Handler) PrivateAsset(c *gin.Context) {
	if _, err := h.svc.Get(c.GetUint("user_id"), c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	h.asset(c)
}
func (h *Handler) asset(c *gin.Context) {
	var a Asset
	err := h.svc.repo.db.First(&a, "id = ? AND article_id = ?", c.Param("asset"), c.Param("id")).Error
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, a.MediaType, a.Data)
}
