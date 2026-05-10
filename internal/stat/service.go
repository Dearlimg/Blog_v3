package stat

import (
	"log/slog"
	"time"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Record(ip string) {
	date := today()
	go func() {
		s.repo.IncrPV(date)
		s.repo.PfaddUV(date, ip)
	}()
}

func (s *Service) Stats() *StatsResp {
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	resp := &StatsResp{}
	resp.Today = s.dayStats(today())
	resp.Yesterday = s.dayStats(yesterday)
	resp.Total.PV, resp.Total.UV = s.repo.SumAllMySQL()
	resp.History = s.repo.ListAllMySQL(30)

	return resp
}

func (s *Service) dayStats(date string) DayStats {
	pv, errPV := s.repo.RedisPV(date)
	uv, errUV := s.repo.RedisUV(date)

	if errPV == nil && errUV == nil {
		return DayStats{PV: pv, UV: uv}
	}

	log, err := s.repo.QueryMySQL(date)
	if err != nil {
		return DayStats{}
	}

	return DayStats{PV: log.PV, UV: log.UV}
}

func (s *Service) Sync() {
	date := today()

	pv, errPV := s.repo.RedisPV(date)
	uv, errUV := s.repo.RedisUV(date)

	if errPV != nil || errUV != nil {
		slog.Warn("stats sync skipped, redis unavailable")
		return
	}

	if pv == 0 && uv == 0 {
		return
	}

	s.repo.UpsertMySQL(date, pv, uv)
	slog.Info("stats synced", "date", date, "pv", pv, "uv", uv)
}

func StartSyncLoop(svc *Service) {
	svc.Sync()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		svc.Sync()
	}
}
