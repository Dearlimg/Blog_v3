package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func Success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": data})
}

func SuccessPage(c *gin.Context, data any, total int64, page, pageSize int) {
	c.JSON(http.StatusOK, gin.H{
		"code": 0, "msg": "success", "data": data,
		"total": total, "page": page, "page_size": pageSize,
	})
}

func Error(c *gin.Context, httpStatus int, code int, msg string) {
	c.JSON(httpStatus, gin.H{"code": code, "msg": msg, "data": nil})
}

func BadRequest(c *gin.Context, msg string)    { Error(c, http.StatusBadRequest, 400, msg) }
func Unauthorized(c *gin.Context, msg string)  { Error(c, http.StatusUnauthorized, 401, msg) }
func Forbidden(c *gin.Context, msg string)     { Error(c, http.StatusForbidden, 403, msg) }
func NotFound(c *gin.Context, msg string)      { Error(c, http.StatusNotFound, 404, msg) }
func InternalError(c *gin.Context, msg string) { Error(c, http.StatusInternalServerError, 500, msg) }
