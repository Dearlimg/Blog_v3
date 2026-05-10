package comment

import (
	"strconv"

	"blog-front/pkg/response"

	"github.com/gin-gonic/gin"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))

	list, total, err := h.svc.List(page, pageSize)
	if err != nil {
		response.InternalError(c, "failed to load comments")
		return
	}

	response.SuccessPage(c, list, total, page, pageSize)
}

func (h *Handler) Create(c *gin.Context) {
	var req CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid input: "+err.Error())
		return
	}

	e, err := h.svc.Create(&req)
	if err != nil {
		response.BadRequest(c, "user not found")
		return
	}

	response.Success(c, e)
}

func (h *Handler) Update(c *gin.Context) {
	userID := c.GetUint("user_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid comment id")
		return
	}

	var req UpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid input: "+err.Error())
		return
	}

	e, err := h.svc.Update(userID, uint(id), &req)
	if err == ErrForbidden {
		response.Forbidden(c, "you can only edit your own comments")
		return
	}
	if err != nil {
		response.NotFound(c, "comment not found")
		return
	}

	response.Success(c, e)
}

func (h *Handler) Delete(c *gin.Context) {
	userID := c.GetUint("user_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid comment id")
		return
	}

	err = h.svc.Delete(userID, uint(id))
	if err == ErrForbidden {
		response.Forbidden(c, "you can only delete your own comments")
		return
	}
	if err != nil {
		response.NotFound(c, "comment not found")
		return
	}

	response.Success(c, nil)
}
