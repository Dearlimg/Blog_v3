package stat

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db  *gorm.DB
	rdb *redis.Client
}

func NewRepository(db *gorm.DB, rdb *redis.Client) *Repository {
	return &Repository{db: db, rdb: rdb}
}

func today() string         { return time.Now().Format("2006-01-02") }
func pvKey(d string) string { return fmt.Sprintf("stat:pv:%s", d) }
func uvKey(d string) string { return fmt.Sprintf("stat:uv:%s", d) }

func (r *Repository) IncrPV(date string) {
	if r.rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r.rdb.Incr(ctx, pvKey(date))
}

func (r *Repository) PfaddUV(date, ip string) {
	if r.rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r.rdb.PFAdd(ctx, uvKey(date), ip)
}

func (r *Repository) RedisPV(date string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return r.rdb.Get(ctx, pvKey(date)).Int64()
}

func (r *Repository) RedisUV(date string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return r.rdb.PFCount(ctx, uvKey(date)).Result()
}

func (r *Repository) UpsertMySQL(date string, pv, uv int64) {
	r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "date"}},
		DoUpdates: clause.Assignments(map[string]any{"pv": gorm.Expr("pv + ?", pv), "uv": gorm.Expr("uv + ?", uv)}),
	}).Create(&VisitLog{Date: date, PV: pv, UV: uv})
}

func (r *Repository) QueryMySQL(date string) (*VisitLog, error) {
	var log VisitLog
	err := r.db.Where("date = ?", date).First(&log).Error
	return &log, err
}

func (r *Repository) SumAllMySQL() (pv, uv int64) {
	r.db.Model(&VisitLog{}).Select("COALESCE(SUM(pv),0), COALESCE(SUM(uv),0)").Row().Scan(&pv, &uv)
	return
}

func (r *Repository) ListAllMySQL(limit int) []VisitLog {
	var logs []VisitLog
	r.db.Order("date DESC").Limit(limit).Find(&logs)
	return logs
}
