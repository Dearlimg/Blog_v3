// The standalone agent server is useful for local development without MySQL.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"blog-front/internal/agent"
	"github.com/gin-gonic/gin"
)

func main() {
	cfg, err := agent.ConfigFromEnv()
	if err != nil {
		slog.Error("agent configuration failed")
		os.Exit(1)
	}
	model, err := agent.NewDeepSeek(cfg)
	if err != nil {
		slog.Error("agent provider configuration failed")
		os.Exit(1)
	}
	svc, err := agent.NewService(cfg, model)
	if err != nil {
		slog.Error("agent storage initialization failed")
		os.Exit(1)
	}
	defer func() { _ = svc.Close() }()
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		slog.Error("invalid trusted proxy configuration")
		return
	}
	r.Static("/view", "./view")
	r.Static("/static", "./static")
	r.GET("/", func(c *gin.Context) { c.File("./view/html/agent.html") })
	svc.Register(r)
	address := os.Getenv("AGENT_LISTEN_ADDR")
	if address == "" {
		address = "127.0.0.1:8083"
	}
	server := &http.Server{Addr: address, Handler: r, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("agent server failed")
			cancel()
		}
	}()
	slog.Info("agent server listening", "address", address)
	<-ctx.Done()
	shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	_ = server.Shutdown(shutdown)
}
