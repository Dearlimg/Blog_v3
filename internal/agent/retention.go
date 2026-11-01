package agent

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

var recordIDPattern = regexp.MustCompile(`[0-9a-f]{48}`)

// Prune operational history only. Approved memories, notes, tasks and teams are durable.
// bbolt reuses freed pages; cleanup does not shrink the database file on disk.
func (s *Store) prune(now time.Time, days int) error {
	cutoff := now.AddDate(0, 0, -days)
	return s.db.Update(func(tx *bolt.Tx) error {
		expired := map[string]map[string]bool{}
		protected := map[string]bool{}
		buckets := []string{"sessions", "runs", "todos", "approvals", "artifacts", "mail", "jobs", "workflows"}
		for _, bucket := range buckets {
			expired[bucket] = map[string]bool{}
			if err := tx.Bucket([]byte(bucket + ".updated")).ForEach(func(id, value []byte) error {
				updated, err := time.Parse(time.RFC3339Nano, string(value))
				if err != nil {
					return err
				}
				if updated.Before(cutoff) {
					expired[bucket][string(id)] = true
				}
				return nil
			}); err != nil {
				return err
			}
		}
		// Keep live operations and the sessions/runs needed to inspect them.
		for _, bucket := range []string{"runs", "jobs", "approvals", "workflows"} {
			if err := tx.Bucket([]byte(bucket)).ForEach(func(id, data []byte) error {
				var record struct {
					Status  string    `json:"status"`
					Session string    `json:"session_id"`
					Run     string    `json:"run_id"`
					Created time.Time `json:"created_at"`
				}
				if err := json.Unmarshal(data, &record); err != nil {
					return err
				}
				live := record.Status == "active" || record.Status == "running" || record.Status == "executing"
				uncertain := (bucket == "approvals" || bucket == "workflows") && record.Status == "interrupted"
				pending := bucket == "approvals" && record.Status == "pending" && now.Sub(record.Created) <= time.Hour
				if live || uncertain || pending || !expired[bucket][string(id)] {
					protected[string(id)] = true
					if record.Session != "" {
						protected[record.Session] = true
					}
					if record.Run != "" {
						protected[record.Run] = true
					}
				}
				return nil
			}); err != nil {
				return err
			}
		}
		// Retain artifacts referenced by surviving conversations and durable workspaces.
		for _, bucket := range []string{"sessions", "teams", "workflows", "tasks", "notes", "memory", "approvals"} {
			if err := tx.Bucket([]byte(bucket)).ForEach(func(id, data []byte) error {
				if expired[bucket][string(id)] && !protected[string(id)] {
					return nil
				}
				for _, reference := range recordIDPattern.FindAll(data, -1) {
					protected[string(reference)] = true
				}
				return nil
			}); err != nil {
				return err
			}
		}
		for bucket, ids := range expired {
			for id := range ids {
				if protected[id] {
					continue
				}
				if err := deleteStoredRecord(tx, bucket, id); err != nil {
					return err
				}
			}
		}
		oldLimits := []string{}
		limitCutoff := now.AddDate(0, 0, -7).UTC().Format("2006-01-02")
		if err := tx.Bucket([]byte("limits")).ForEach(func(key, _ []byte) error {
			day, _, ok := strings.Cut(string(key), ":")
			if ok && day < limitCutoff {
				oldLimits = append(oldLimits, string(key))
			}
			return nil
		}); err != nil {
			return err
		}
		for _, id := range oldLimits {
			if err := deleteStoredRecord(tx, "limits", id); err != nil {
				return err
			}
		}
		return nil
	})
}
