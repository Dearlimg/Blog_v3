package wallet

import (
	"log/slog"
	"strconv"

	"blog-front/pkg/response"

	"github.com/gin-gonic/gin"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) GetOrCreate(c *gin.Context) {
	userID, err := parseUserID(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	e, err := h.svc.GetOrCreate(userID)
	if err != nil {
		response.InternalError(c, "wallet error")
		return
	}

	response.Success(c, e)
}

func (h *Handler) AddBalance(c *gin.Context) {
	userID, err := parseUserID(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	var req AddBalanceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid input: "+err.Error())
		return
	}

	e, txn, err := h.svc.AddBalance(userID, &req)
	if err != nil {
		slog.Error("add balance failed", "error", err)
		response.InternalError(c, "failed to add balance")
		return
	}

	response.Success(c, gin.H{"wallet": e, "transaction": txn})
}

func (h *Handler) Transfer(c *gin.Context) {
	userID, err := parseUserID(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	var req TransferReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid input: "+err.Error())
		return
	}

	from, to, txns, err := h.svc.Transfer(userID, &req)
	if err == ErrSelfTransfer || err == ErrBalance {
		response.BadRequest(c, err.Error())
		return
	}
	if err != nil {
		slog.Error("transfer failed", "error", err)
		response.InternalError(c, "transfer failed")
		return
	}

	response.Success(c, gin.H{"from_wallet": from, "to_wallet": to, "amount": req.Amount, "transactions": txns})
}

func (h *Handler) Transactions(c *gin.Context) {
	userID, err := parseUserID(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	txns, err := h.svc.Transactions(userID)
	if err != nil {
		response.NotFound(c, "wallet not found")
		return
	}

	response.Success(c, txns)
}

func parseUserID(c *gin.Context) (uint, error) {
	idStr := c.Param("userId")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(id), nil
}
