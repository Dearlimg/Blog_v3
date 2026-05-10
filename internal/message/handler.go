package message

import (
	"log/slog"

	"blog-front/pkg/response"

	"github.com/gin-gonic/gin"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) List(c *gin.Context) {
	list, err := h.svc.List()
	if err != nil {
		response.InternalError(c, "failed to load messages")
		return
	}

	response.Success(c, list)
}

func (h *Handler) Create(c *gin.Context) {
	var req CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid input: "+err.Error())
		return
	}

	e, err := h.svc.Create(&req, c.ClientIP())
	if err != nil {
		slog.Error("create message failed", "error", err)
		response.InternalError(c, "failed to create message")
		return
	}

	response.Success(c, e)
}
