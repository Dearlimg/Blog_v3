package order

import (
	"log/slog"
	"strconv"

	"blog-front/pkg/response"

	"github.com/gin-gonic/gin"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// ---- Order ----

func (h *Handler) CreateOrder(c *gin.Context) {
	userID := c.GetUint("user_id")

	var req CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid input: "+err.Error())
		return
	}

	o, err := h.svc.CreateOrder(userID, &req)
	if err == ErrStock {
		response.BadRequest(c, err.Error())
		return
	}
	if err != nil {
		slog.Error("create order failed", "error", err)
		response.InternalError(c, "failed to create order")
		return
	}

	response.Success(c, o)
}

func (h *Handler) ListOrders(c *gin.Context) {
	userID := c.GetUint("user_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	list, total, err := h.svc.ListOrders(userID, page, pageSize)
	if err != nil {
		response.InternalError(c, "failed to list orders")
		return
	}

	response.SuccessPage(c, list, total, page, pageSize)
}

func (h *Handler) GetOrder(c *gin.Context) {
	userID := c.GetUint("user_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid order id")
		return
	}

	o, err := h.svc.OrderByID(userID, uint(id))
	if err != nil {
		response.NotFound(c, "order not found")
		return
	}

	response.Success(c, o)
}

// ---- Cart ----

func (h *Handler) CartItems(c *gin.Context) {
	userID := c.GetUint("user_id")

	items, err := h.svc.CartItems(userID)
	if err != nil {
		response.InternalError(c, "failed to load cart")
		return
	}

	response.Success(c, items)
}

func (h *Handler) AddCartItem(c *gin.Context) {
	userID := c.GetUint("user_id")

	var req AddCartReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid input: "+err.Error())
		return
	}

	item, err := h.svc.AddCartItem(userID, &req)
	if err != nil {
		response.NotFound(c, "product not found")
		return
	}

	response.Success(c, item)
}

func (h *Handler) UpdateCartItem(c *gin.Context) {
	userID := c.GetUint("user_id")
	itemID, err := strconv.ParseUint(c.Param("itemId"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid item id")
		return
	}

	var req UpdateCartReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid input: "+err.Error())
		return
	}

	item, err := h.svc.UpdateCartItem(userID, uint(itemID), &req)
	if err == ErrForbidden {
		response.Forbidden(c, "you can only update your own cart")
		return
	}
	if err != nil {
		response.NotFound(c, "cart item not found")
		return
	}

	response.Success(c, item)
}

func (h *Handler) RemoveCartItem(c *gin.Context) {
	userID := c.GetUint("user_id")
	itemID, err := strconv.ParseUint(c.Param("itemId"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid item id")
		return
	}

	err = h.svc.RemoveCartItem(userID, uint(itemID))
	if err == ErrForbidden {
		response.Forbidden(c, "you can only manage your own cart")
		return
	}
	if err != nil {
		response.NotFound(c, "cart item not found")
		return
	}

	response.Success(c, nil)
}

func (h *Handler) Checkout(c *gin.Context) {
	userID := c.GetUint("user_id")

	var req CheckoutReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid input: "+err.Error())
		return
	}

	orders, total, err := h.svc.Checkout(userID, &req)
	if err == ErrEmptyCart {
		response.BadRequest(c, err.Error())
		return
	}
	if err != nil {
		slog.Error("checkout failed", "error", err)
		response.BadRequest(c, err.Error())
		return
	}

	response.Success(c, gin.H{"orders": orders, "total": total})
}
