package main

import (
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"blog-front/config"
	"blog-front/internal/comment"
	"blog-front/internal/message"
	"blog-front/internal/order"
	"blog-front/internal/product"
	"blog-front/internal/user"
	"blog-front/internal/wallet"
	"blog-front/pkg/database"
	"blog-front/router"
)

const configPath = "config/config.yaml"

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("load config failed", "error", err)
		os.Exit(1)
	}

	db, err := database.NewMySQL(&cfg.Database)
	if err != nil {
		slog.Error("database init failed", "error", err)
		os.Exit(1)
	}

	if err := db.AutoMigrate(
		&user.Entity{},
		&comment.Entity{},
		&message.Entity{},
		&product.Entity{},
		&order.Entity{},
		&wallet.Entity{},
		&wallet.Transaction{},
		&order.CartItem{},
	); err != nil {
		slog.Error("auto migrate failed", "error", err)
		os.Exit(1)
	}

	slog.Info("database migration completed")

	if _, err := database.NewRedis(&cfg.Redis); err != nil {
		slog.Warn("redis init failed, continuing without redis", "error", err)
	}

	r := router.Setup(cfg, db)

	addr := ":" + strconv.Itoa(cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
	}

	slog.Info("server starting", "port", cfg.Server.Port)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server start failed", "error", err)
		os.Exit(1)
	}
}
