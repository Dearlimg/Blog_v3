// Command article-preview runs an isolated local article preview without MySQL or Redis.
package main

import (
	"flag"
	"log"
	"net"
	"os"
	"path/filepath"

	"blog-front/internal/article"
	"blog-front/internal/user"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:8086", "loopback address")
	path := flag.String("db", "work/articles-preview.db", "isolated SQLite preview database")
	flag.Parse()
	host, _, err := net.SplitHostPort(*addr)
	if err != nil || host != "127.0.0.1" {
		log.Fatal("preview must bind to 127.0.0.1")
	}
	password := os.Getenv("ARTICLE_PREVIEW_PASSWORD")
	if len(password) < 12 {
		log.Fatal("set ARTICLE_PREVIEW_PASSWORD to at least 12 characters")
	}
	if err := os.MkdirAll(filepath.Dir(*path), 0700); err != nil {
		log.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(*path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		log.Fatal(err)
	}
	connection, err := db.DB()
	if err != nil {
		log.Fatal(err)
	}
	connection.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&user.Entity{}, &article.Entity{}, &article.Draft{},
		&article.Revision{}, &article.Asset{}, &article.Receipt{}); err != nil {
		log.Fatal(err)
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal(err)
	}
	for _, u := range []user.Entity{
		{ID: 1, Username: "Preview Owner", Email: "owner@example.test", Password: string(hashed)},
		{ID: 2, Username: "Preview Reader", Email: "reader@example.test", Password: string(hashed)},
	} {
		if err := db.Save(&u).Error; err != nil {
			log.Fatal(err)
		}
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Static("/static", "./static")
	r.Static("/view", "./view")
	r.GET("/", func(c *gin.Context) { c.File("./index.html") })
	secret := "local-preview-" + password
	h := user.NewHandler(user.NewService(db, secret, 7200))
	r.POST("/api/v1/users/login", h.Login)
	article.Register(r, db, secret, 1)
	log.Printf("Local article preview: http://%s (owner@example.test)", *addr)
	log.Fatal(r.Run(*addr))
}
