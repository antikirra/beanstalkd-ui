package store

import (
	"encoding/json"
	"time"

	"github.com/antikirra/beanstalkd-ui/internal/model"
	bolt "go.etcd.io/bbolt"
)

const Version = 2.2

var (
	bucketServers = []byte("servers")
	bucketSamples = []byte("samples")
	keySampleData = []byte("data")
)

// Store provides persistent storage backed by bbolt.
type Store struct {
	db *bolt.DB
}

// Open opens (or creates) the bbolt database at path and ensures required buckets exist.
func Open(path string) (*Store, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(bucketServers); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketSamples); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// ListServers returns all server addresses sorted alphabetically (bbolt keys are byte-sorted).
func (s *Store) ListServers() ([]string, error) {
	var servers []string
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketServers)
		return b.ForEach(func(k, _ []byte) error {
			servers = append(servers, string(k))
			return nil
		})
	})
	return servers, err
}

// AddServer adds a server address to the store.
func (s *Store) AddServer(addr string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketServers).Put([]byte(addr), nil)
	})
}

// RemoveServer removes a server address from the store.
func (s *Store) RemoveServer(addr string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketServers).Delete([]byte(addr))
	})
}

// LoadSamples reads sample jobs from the store. Returns empty SampleJobs if no data exists.
func (s *Store) LoadSamples() (model.SampleJobs, error) {
	var sj model.SampleJobs
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketSamples).Get(keySampleData)
		if data == nil {
			return nil
		}
		return json.Unmarshal(data, &sj)
	})
	return sj, err
}

// SaveSamples persists sample jobs to the store.
func (s *Store) SaveSamples(sj model.SampleJobs) error {
	data, err := json.Marshal(sj)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketSamples).Put(keySampleData, data)
	})
}
