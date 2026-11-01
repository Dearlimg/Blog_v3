package agent

import (
	"bytes"
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
		return initializeIndexes(tx)
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
	if err := indexRecord(tx, bucket, id, tx.Bucket([]byte(bucket)).Get([]byte(id)), false); err != nil {
		return err
	}
	if err := tx.Bucket([]byte(bucket)).Put([]byte(id), data); err != nil {
		return err
	}
	return indexRecord(tx, bucket, id, data, true)
}

var indexedBuckets = []string{
	"sessions", "runs", "memory", "notes", "todos", "tasks", "jobs",
	"teams", "mail", "workflows", "approvals", "artifacts", "limits",
}

var indexFields = []string{"principal", "session_id", "status", "to"}

func initializeIndexes(tx *bolt.Tx) error {
	meta, err := tx.CreateBucketIfNotExists([]byte("schema"))
	if err != nil {
		return err
	}
	if bytes.Equal(meta.Get([]byte("indexes")), []byte("1")) {
		return nil
	}
	for _, bucket := range indexedBuckets {
		for _, field := range append([]string{"updated"}, indexFields...) {
			if _, err := tx.CreateBucketIfNotExists([]byte(bucket + "." + field)); err != nil {
				return err
			}
		}
		if err := tx.Bucket([]byte(bucket)).ForEach(func(key, data []byte) error {
			return indexRecord(tx, bucket, string(key), data, true)
		}); err != nil {
			return err
		}
	}
	return meta.Put([]byte("indexes"), []byte("1"))
}

func indexRecord(tx *bolt.Tx, bucket, id string, data []byte, add bool) error {
	fields := map[string]json.RawMessage{}
	if len(data) > 0 && bucket != "limits" {
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
	}
	for _, field := range indexFields {
		var value string
		if json.Unmarshal(fields[field], &value) != nil || value == "" {
			continue
		}
		index := tx.Bucket([]byte(bucket + "." + field))
		key := []byte(value + "\x00" + id)
		var err error
		if add {
			err = index.Put(key, []byte(id))
		} else {
			err = index.Delete(key)
		}
		if err != nil {
			return err
		}
	}
	updated := tx.Bucket([]byte(bucket + ".updated"))
	if add {
		return updated.Put([]byte(id), []byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return updated.Delete([]byte(id))
}

func deleteStoredRecord(tx *bolt.Tx, bucket, id string) error {
	if err := indexRecord(tx, bucket, id, tx.Bucket([]byte(bucket)).Get([]byte(id)), false); err != nil {
		return err
	}
	return tx.Bucket([]byte(bucket)).Delete([]byte(id))
}

func indexedRecords[T any](s *Store, bucket, field, value string) ([]T, error) {
	items := []T{}
	err := s.db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket([]byte(bucket + "." + field)).Cursor()
		prefix := []byte(value + "\x00")
		for key, id := cursor.Seek(prefix); bytes.HasPrefix(key, prefix); key, id = cursor.Next() {
			var item T
			if err := readRecord(tx, bucket, string(id), &item); err != nil {
				return err
			}
			items = append(items, item)
		}
		return nil
	})
	return items, err
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
