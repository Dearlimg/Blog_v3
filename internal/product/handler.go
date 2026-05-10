package product

import (
	"log/slog"
	"strconv"

	"blog-front/pkg/response"

	"github.com/gin-gonic/gin"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "100"))

	list, total, err := h.svc.List(page, pageSize, c.Query("category"), c.Query("keyword"))
	if err != nil {
		response.InternalError(c, "failed to list products")
		return
	}

	response.SuccessPage(c, list, total, page, pageSize)
}

func (h *Handler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid product id")
		return
	}

	e, err := h.svc.ByID(uint(id))
	if err != nil {
		response.NotFound(c, "product not found")
		return
	}

	response.Success(c, e)
}

func (h *Handler) Create(c *gin.Context) {
	var e Entity
	if err := c.ShouldBindJSON(&e); err != nil {
		response.BadRequest(c, "invalid input: "+err.Error())
		return
	}

	if err := h.svc.Create(&e); err != nil {
		slog.Error("create product failed", "error", err)
		response.InternalError(c, "failed to create product")
		return
	}

	response.Success(c, e)
}

func (h *Handler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid product id")
		return
	}

	var updates map[string]any
	if err := c.ShouldBindJSON(&updates); err != nil {
		response.BadRequest(c, "invalid input: "+err.Error())
		return
	}

	if err := h.svc.Update(uint(id), updates); err != nil {
		response.NotFound(c, "product not found")
		return
	}

	e, _ := h.svc.ByID(uint(id))
	response.Success(c, e)
}

func (h *Handler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid product id")
		return
	}

	if err := h.svc.Delete(uint(id)); err != nil {
		response.NotFound(c, "product not found")
		return
	}

	response.Success(c, nil)
}
