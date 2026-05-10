package user

import (
	"log/slog"

	"blog-front/pkg/response"

	"github.com/gin-gonic/gin"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(c *gin.Context) {
	var req RegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid input: "+err.Error())
		return
	}

	u, err := h.svc.Register(&req)
	if err == ErrDuplicateEmail {
		response.BadRequest(c, err.Error())
		return
	}
	if err != nil {
		slog.Error("register failed", "error", err)
		response.InternalError(c, "failed to create user")
		return
	}

	response.Success(c, gin.H{"id": u.ID, "username": u.Username, "email": u.Email})
}

func (h *Handler) Login(c *gin.Context) {
	var req LoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid input: "+err.Error())
		return
	}

	resp, err := h.svc.Login(&req)
	if err == ErrInvalidCred {
		response.BadRequest(c, err.Error())
		return
	}
	if err != nil {
		slog.Error("login failed", "error", err)
		response.InternalError(c, "login failed")
		return
	}

	response.Success(c, resp)
}

func (h *Handler) SendCode(c *gin.Context) {
	var req SendCodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid input: "+err.Error())
		return
	}

	if err := h.svc.SendCode(req.Email); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	slog.Info("verification code sent", "email", req.Email)
	response.Success(c, gin.H{"message": "verification code sent"})
}

func (h *Handler) VerifyEmail(c *gin.Context) {
	var req VerifyEmailReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid input: "+err.Error())
		return
	}

	if err := h.svc.VerifyEmail(req.Email, req.Code); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	response.Success(c, gin.H{"message": "email verified successfully"})
}

func (h *Handler) Profile(c *gin.Context) {
	userID := c.GetUint("user_id")

	u, err := h.svc.Profile(userID)
	if err != nil {
		response.NotFound(c, "user not found")
		return
	}

	response.Success(c, u)
}

func HealthCheck(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok", "message": "blog-front backend is running"})
}
