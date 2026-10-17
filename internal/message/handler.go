package message

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"blog-front/pkg/response"

	"github.com/gin-gonic/gin"
)

const (
	defaultPageSize = 20
	maxPageSize     = 50
	maxPage         = 10000
	maxRequestBytes = 16 * 1024
)

type Handler struct {
	svc     *Service
	limiter *CreateRateLimiter
}

func NewHandler(svc *Service, limiters ...*CreateRateLimiter) *Handler {
	h := &Handler{svc: svc}
	if len(limiters) > 0 {
		h.limiter = limiters[0]
	}
	return h
}

func (h *Handler) List(c *gin.Context) {
	page, pageSize, ok := pagination(c)
	if !ok {
		return
	}

	list, total, err := h.svc.List(page, pageSize)
	if err != nil {
		response.InternalError(c, "failed to load messages")
		return
	}

	response.SuccessPage(c, list, total, page, pageSize)
}

func (h *Handler) Create(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBytes)
	var req CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			response.Error(c, http.StatusRequestEntityTooLarge, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		response.BadRequest(c, "invalid input")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.TrimSpace(req.Email)
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		response.BadRequest(c, "message content is required")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
	defer cancel()
	ip := c.ClientIP()
	retryAfter, err := h.limiter.RetryAfter(ctx, ip)
	if err != nil {
		slog.Error("check message rate limit failed", "error", err)
		response.Error(c, http.StatusServiceUnavailable, http.StatusServiceUnavailable, "message submission is temporarily unavailable")
		return
	}
	if retryAfter > 0 {
		retrySeconds := (retryAfter + time.Second - 1) / time.Second
		c.Header("Retry-After", strconv.FormatInt(int64(retrySeconds), 10))
		response.Error(c, http.StatusTooManyRequests, http.StatusTooManyRequests, "please wait before submitting another message")
		return
	}

	e, err := h.svc.Create(&req, ip)
	if err != nil {
		slog.Error("create message failed", "error", err)
		response.InternalError(c, "failed to create message")
		return
	}

	response.Success(c, e)
}

func pagination(c *gin.Context) (int, int, bool) {
	page := 1
	if value := c.Query("page"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > maxPage {
			response.BadRequest(c, "page must be between 1 and 10000")
			return 0, 0, false
		}
		page = parsed
	}

	pageSize := defaultPageSize
	if value := c.Query("page_size"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			response.BadRequest(c, "page_size must be a positive integer")
			return 0, 0, false
		}
		pageSize = parsed
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize, true
}
