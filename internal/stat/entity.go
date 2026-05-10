package stat

type VisitLog struct {
	ID   uint   `json:"id" gorm:"primaryKey"`
	Date string `json:"date" gorm:"uniqueIndex;size:10;not null"`
	PV   int64  `json:"pv" gorm:"default:0"`
	UV   int64  `json:"uv" gorm:"default:0"`
}

func (VisitLog) TableName() string { return "visit_logs" }

type DayStats struct {
	PV int64 `json:"pv"`
	UV int64 `json:"uv"`
}

type StatsResp struct {
	Today     DayStats   `json:"today"`
	Yesterday DayStats   `json:"yesterday"`
	Total     DayStats   `json:"total"`
	History   []VisitLog `json:"history"`
}
