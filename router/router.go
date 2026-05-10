package router

import (
	"blog-front/config"
	"blog-front/internal/comment"
	"blog-front/internal/message"
	"blog-front/internal/order"
	"blog-front/internal/product"
	"blog-front/internal/user"
	"blog-front/internal/wallet"
	"blog-front/pkg/middleware"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func Setup(cfg *config.Config, db *gorm.DB) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Logger())

	r.Static("/static", "./static")
	r.Static("/view", "./view")
	r.StaticFile("/index.html", "./index.html")
	r.GET("/", func(c *gin.Context) { c.File("./index.html") })

	userSvc := user.NewService(db, cfg.JWT.SecretKey, cfg.JWT.Expire)
	commentSvc := comment.NewService(db)
	messageSvc := message.NewService(db)
	productSvc := product.NewService(db)
	orderSvc := order.NewService(db)
	walletSvc := wallet.NewService(db)

	userH := user.NewHandler(userSvc)
	commentH := comment.NewHandler(commentSvc)
	messageH := message.NewHandler(messageSvc)
	productH := product.NewHandler(productSvc)
	orderH := order.NewHandler(orderSvc)
	walletH := wallet.NewHandler(walletSvc)

	r.GET("/health", user.HealthCheck)

	api := r.Group("/api/v1")
	{
		users := api.Group("/users")
		{
			users.POST("/register", userH.Register)
			users.POST("/login", userH.Login)
			users.POST("/send-verification-code", userH.SendCode)
			users.POST("/verify-email", userH.VerifyEmail)
		}

		api.GET("/comments", commentH.List)
		api.GET("/messages", messageH.List)
		api.POST("/messages", messageH.Create)
		api.GET("/products", productH.List)
		api.GET("/products/:id", productH.Get)

		auth := api.Group("")
		auth.Use(middleware.Auth(cfg.JWT.SecretKey))
		{
			auth.GET("/users/profile", userH.Profile)

			auth.POST("/comments", commentH.Create)
			auth.PUT("/comments/:id", commentH.Update)
			auth.DELETE("/comments/:id", commentH.Delete)

			auth.GET("/cart", orderH.CartItems)
			auth.POST("/cart/items", orderH.AddCartItem)
			auth.PUT("/cart/items/:itemId", orderH.UpdateCartItem)
			auth.DELETE("/cart/items/:itemId", orderH.RemoveCartItem)
			auth.POST("/cart/checkout", orderH.Checkout)

			auth.POST("/orders", orderH.CreateOrder)
			auth.GET("/orders", orderH.ListOrders)
			auth.GET("/orders/:id", orderH.GetOrder)

			auth.GET("/wallets/:userId", walletH.GetOrCreate)
			auth.POST("/wallets/:userId", walletH.GetOrCreate)
			auth.POST("/wallets/:userId/balance", walletH.AddBalance)
			auth.POST("/wallets/:userId/transfer", walletH.Transfer)
			auth.GET("/wallets/:userId/transactions", walletH.Transactions)
		}
	}

	return r
}
