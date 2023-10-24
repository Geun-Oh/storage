package scylladb

import (
	"context"
	"errors"
	"fmt"
	"github.com/gocql/gocql"
	"time"
)

type Storage struct {
	Cluster *gocql.ClusterConfig
	Session *gocql.Session

	cqlSelect string
	cqlInsert string
	cqlDelete string
	cqlReset  string
	cqlGC     string
}

var (
	createKeyspaceQuery = "CREATE KEYSPACE IF NOT EXISTS %s WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};"

	dropQuery = "DROP TABLE IF EXISTS %s;"

	initQuery = []string{
		`CREATE TABLE IF NOT EXISTS %s (
			k  text PRIMARY KEY,
			v  blob,
			e  bigint
		);`,
	}
)

func New(config ...Config) *Storage {
	cfg := configDefault(config...)

	cluster := gocql.NewCluster(cfg.Hosts...)
	cluster.Consistency = gocql.ParseConsistency(cfg.Consistency)
	cluster.Keyspace = cfg.Keyspace

	session, err := cluster.CreateSession()
	if err != nil {
		panic(err)
	}

	// Primitive ping method
	if err := session.Query("SELECT release_version FROM system.local").Exec(); err != nil {
		session.Close()
		panic(err)
	}

	// Create keyspace if it does not exist
	if err := session.Query(fmt.Sprintf(createKeyspaceQuery, cfg.Keyspace)).Exec(); err != nil {
		session.Close()
		panic(err)
	}

	// Drop table if reset set
	ctx := context.Background()
	if cfg.Reset {
		if err := session.Query(dropQuery, cfg.Table).WithContext(ctx).Exec(); err != nil {
			session.Close()
			panic(err)
		}
	}

	// Init database queries
	ctx = context.Background()
	for _, query := range initQuery {

		if err := session.Query(fmt.Sprintf(query, cfg.Table)).WithContext(ctx).Exec(); err != nil {
			session.Close()
			panic(err)
		}
	}

	storage := &Storage{
		Cluster: cluster,
		Session: session,

		cqlSelect: fmt.Sprintf(`SELECT v, e FROM %s WHERE k=?`, cfg.Table),
		cqlInsert: fmt.Sprintf(`INSERT INTO %s (k, v, e) VALUES (?, ?, ?)`, cfg.Table),
		cqlDelete: fmt.Sprintf(`DELETE FROM %s WHERE k=?`, cfg.Table),
		cqlReset:  fmt.Sprintf(`TRUNCATE %s`, cfg.Table),
		cqlGC:     fmt.Sprintf(`DELETE FROM %s WHERE e <= ? AND e != 0`, cfg.Table),
	}

	return storage
}

// Get value by key
func (s *Storage) Get(key string) ([]byte, error) {
	ctx := context.Background()
	var (
		data []byte
		exp  int64 = 0
	)
	if err := s.Session.Query(s.cqlSelect, key).WithContext(ctx).Scan(&data, &exp); err != nil {
		if errors.Is(err, gocql.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return data, nil
}

// Set sets a value in the storage for the provided key
func (s *Storage) Set(key string, val []byte, exp time.Duration) error {
	ctx := context.Background()

	return s.Session.Query(s.cqlInsert, key, val, int64(exp.Seconds())).WithContext(ctx).Exec()
}

// Delete deletes a value from the storage based on the provided key
func (s *Storage) Delete(key string) error {
	ctx := context.Background()

	return s.Session.Query(s.cqlDelete, key).WithContext(ctx).Exec()
}

// Reset resets the storage
func (s *Storage) Reset() error {
	ctx := context.Background()

	return s.Session.Query(s.cqlReset).WithContext(ctx).Exec()
}

// Close closes the connection to the storage
func (s *Storage) Close() error {
	s.Session.Close()
	return nil
}

// Conn returns session
func (s *Storage) Conn() *gocql.Session {
	return s.Session
}
