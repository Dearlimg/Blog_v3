package router

import (
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"blog-front/config"
	"blog-front/internal/agent"
	"blog-front/internal/article"
	"blog-front/internal/comment"
	"blog-front/internal/message"
	"blog-front/internal/order"
	"blog-front/internal/product"
	"blog-front/internal/stat"
	"blog-front/internal/user"
	"blog-front/internal/wallet"
	"blog-front/pkg/middleware"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type homepageMetadata struct {
	CanonicalURL string
	ImageURL     string
}

func homepageBaseURL(siteURL string, request *http.Request) string {
	if siteURL != "" {
		return strings.TrimRight(siteURL, "/")
	}

	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + request.Host
}

func Setup(cfg *config.Config, db *gorm.DB, rdb *redis.Client) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	statRepo := stat.NewRepository(db, rdb)
	statSvc := stat.NewService(statRepo)
	statH := stat.NewHandler(statSvc)

	r := gin.New()
	trustedProxies := cfg.TrustedProxies
	if len(trustedProxies) == 0 {
		trustedProxies = nil
	}
	if err := r.SetTrustedProxies(trustedProxies); err != nil {
		slog.Error("configure trusted proxies failed; forwarding headers will be ignored", "error", err)
		_ = r.SetTrustedProxies(nil)
	}
	r.Use(gin.Recovery())
	r.Use(middleware.Logger())
	r.Use(stat.Middleware(statSvc))

	r.Static("/static", "./static")
	r.Static("/view", "./view")
	homeTemplate, err := template.ParseFiles("./index.html")
	if err != nil {
		slog.Error("load homepage template failed", "error", err)
	}
	homePage := func(c *gin.Context) {
		if homeTemplate == nil {
			c.String(http.StatusInternalServerError, "homepage unavailable")
			return
		}
		baseURL := homepageBaseURL(cfg.SiteURL, c.Request)
		c.Header("Content-Type", "text/html; charset=utf-8")
		if err := homeTemplate.ExecuteTemplate(c.Writer, "index.html", homepageMetadata{
			CanonicalURL: baseURL + "/",
			ImageURL:     baseURL + "/static/Ava.jpg",
		}); err != nil {
			slog.Error("render homepage failed", "error", err)
		}
	}
	r.GET("/index.html", homePage)
	r.GET("/agent", func(c *gin.Context) { c.File("./view/html/agent.html") })
	r.GET("/", homePage)
	article.Register(r, db, cfg.JWT.SecretKey, cfg.BlogOwnerUserID, cfg.SiteURL)

	if err := agent.RegisterFromEnv(r); err != nil {
		slog.Error("agent initialization failed; agent endpoints disabled")
	}

	userSvc := user.NewService(db, cfg.JWT.SecretKey, cfg.JWT.Expire)
	commentSvc := comment.NewService(db)
	messageSvc := message.NewService(db)
	productSvc := product.NewService(db)
	orderSvc := order.NewService(db)
	walletSvc := wallet.NewService(db)

	userH := user.NewHandler(userSvc)
	commentH := comment.NewHandler(commentSvc)
	messageH := message.NewHandler(messageSvc, message.NewCreateRateLimiter(rdb))
	productH := product.NewHandler(productSvc)
	orderH := order.NewHandler(orderSvc)
	walletH := wallet.NewHandler(walletSvc)

	r.GET("/health", user.HealthCheck)

	api := r.Group("/api/v1")
	{
		api.GET("/stats", statH.Stats)
		api.GET("/stats/details", statH.Details)

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
