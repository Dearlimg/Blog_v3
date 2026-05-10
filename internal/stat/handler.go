package stat

import (
	"blog-front/pkg/response"

	"github.com/gin-gonic/gin"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Stats(c *gin.Context) {
	response.Success(c, h.svc.Stats())
}
