package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

// Store is an atomic, single-process persistent store, separate from blog data.
type Store struct{ db *bolt.DB }

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	// bbolt 是独占 flock：k8s 滚动更新新旧 pod 重叠期间，旧 pod 可能仍持有锁。
	// 等待窗口给足 15s（bbolt 内部会轮询锁），配合 deployment maxSurge=0 双保险。
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: 15 * time.Second})
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range []string{"sessions", "runs", "memory", "notes", "todos", "tasks", "jobs", "teams", "mail", "workflows", "approvals", "artifacts", "limits"} {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) get(bucket, id string, target any) error {
	return s.db.View(func(tx *bolt.Tx) error { return readRecord(tx, bucket, id, target) })
}

func readRecord(tx *bolt.Tx, bucket, id string, target any) error {
	value := tx.Bucket([]byte(bucket)).Get([]byte(id))
	if value == nil {
		return ErrNotFound
	}
	return json.Unmarshal(value, target)
}

func writeRecord(tx *bolt.Tx, bucket, id string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return tx.Bucket([]byte(bucket)).Put([]byte(id), data)
}

func (s *Store) put(bucket, id string, value any) error {
	return s.db.Update(func(tx *bolt.Tx) error { return writeRecord(tx, bucket, id, value) })
}

func records[T any](s *Store, bucket string) ([]T, error) {
	items := []T{}
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucket)).ForEach(func(_, value []byte) error {
			var item T
			if err := json.Unmarshal(value, &item); err != nil {
				return err
			}
			items = append(items, item)
			return nil
		})
	})
	return items, err
}

func updateRecord[T any](s *Store, bucket, id string, change func(*T) error) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		var item T
		if err := readRecord(tx, bucket, id, &item); err != nil {
			return err
		}
		if err := change(&item); err != nil {
			return err
		}
		return writeRecord(tx, bucket, id, item)
	})
}
