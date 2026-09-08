package article

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // Register the image formats accepted by uploads.
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strconv"
	"strings"

	"blog-front/internal/user"
	"blog-front/pkg/middleware"
	"blog-front/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Handler struct{ svc *Service }

func Register(r *gin.Engine, db *gorm.DB, secret string, owner uint) {
	h := &Handler{svc: NewService(db)}
	r.GET("/articles", func(c *gin.Context) { c.File("./view/html/articles.html") })
	r.GET("/articles/:id", func(c *gin.Context) { c.File("./view/html/article.html") })
	r.GET("/studio/articles", func(c *gin.Context) { c.File("./view/html/studio.html") })
	r.GET("/studio/articles/:id/edit", func(c *gin.Context) { c.File("./view/html/editor.html") })
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
		defer c.Request.MultipartForm.RemoveAll()
	}
	if err != nil {
		response.BadRequest(c, "image required (max 5 MB)")
		return
	}
	defer file.Close()
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
