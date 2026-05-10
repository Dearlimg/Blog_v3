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
	"blog-front/internal/stat"
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

	rdb, err := database.NewRedis(&cfg.Redis)
	if err != nil {
		slog.Warn("redis init failed, stats will use mysql fallback", "error", err)
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
		&stat.VisitLog{},
	); err != nil {
		slog.Error("auto migrate failed", "error", err)
		os.Exit(1)
	}

	slog.Info("database migration completed")

	statRepo := stat.NewRepository(db, rdb)
	statSvc := stat.NewService(statRepo)

	go stat.StartSyncLoop(statSvc)

	r := router.Setup(cfg, db, rdb)

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
