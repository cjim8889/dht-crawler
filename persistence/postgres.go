package persistence

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"time"
	"unicode/utf8"

	"net/http"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/httpfs"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

//go:embed postgres_migrations/*
var migrationFiles embed.FS

type postgresDatabase struct {
	conn   *pgxpool.Pool
	schema string
}

func makePostgresDatabase(dsn string) (Database, error) {
	conn, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}

	db := new(postgresDatabase)
	db.schema = "dht_crawler"
	db.conn = conn

	zap.L().Info("Setting up the database.")
	if err := db.setupDatabase(dsn); err != nil {
		return nil, errors.Wrap(err, "setupDatabase")
	}
	zap.L().Info("The database has been set up.")

	return db, nil
}

func (db *postgresDatabase) Engine() databaseEngine {
	return Postgres
}

func (db *postgresDatabase) DoesTorrentExist(infoHash []byte) (bool, error) {
	rows, err := db.conn.Query(context.Background(), "SELECT 1 FROM torrents WHERE info_hash = $1;", infoHash)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	exists := rows.Next()
	if rows.Err() != nil {
		return false, err
	}

	return exists, nil
}

func (db *postgresDatabase) AddNewTorrent(infoHash []byte, name string, files []File) error {
	if !utf8.ValidString(name) {
		zap.L().Warn(
			"Ignoring a torrent whose name is not UTF-8 compliant.",
			zap.ByteString("infoHash", infoHash),
			zap.Binary("name", []byte(name)),
		)

		return nil
	}

	tx, err := db.conn.Begin(context.Background())
	if err != nil {
		return errors.Wrap(err, "conn.Begin")
	}
	// If everything goes as planned and no error occurs, we will commit the transaction before
	// returning from the function so the tx.Rollback() call will fail, trying to rollback a
	// committed transaction. BUT, if an error occurs, we'll get our transaction rollback'ed, which
	// is nice.
	defer tx.Rollback(context.Background())

	var totalSize uint64 = 0
	for _, file := range files {
		totalSize += uint64(file.Size)
	}

	// This is a workaround for a bug: the database will not accept total_size to be zero.
	if totalSize == 0 {
		zap.L().Debug("Ignoring a torrent whose total size is zero.")
		return nil
	}

	if exist, err := db.DoesTorrentExist(infoHash); exist || err != nil {
		return err
	}

	var lastInsertId int64

	err = tx.QueryRow(
		context.Background(),
		`
		INSERT INTO torrents (
			info_hash,
			name,
			total_size,
			discovered_on
		) VALUES ($1, $2, $3, $4)
		RETURNING id;
	`, infoHash, name, totalSize, time.Now().Unix()).Scan(&lastInsertId)
	if err != nil {
		return errors.Wrap(err, "tx.QueryRow (INSERT INTO torrents)")
	}

	for _, file := range files {
		if !utf8.ValidString(file.Path) {
			zap.L().Warn(
				"Ignoring a file whose path is not UTF-8 compliant.",
				zap.Binary("path", []byte(file.Path)),
			)

			// Returning nil so deferred tx.Rollback() will be called and transaction will be canceled.
			return nil
		}

		_, err = tx.Exec(
			context.Background(),
			"INSERT INTO files (torrent_id, size, path) VALUES ($1, $2, $3);",
			lastInsertId, file.Size, file.Path,
		)
		if err != nil {
			return errors.Wrap(err, "tx.Exec (INSERT INTO files)")
		}
	}

	err = tx.Commit(context.Background())
	if err != nil {
		return errors.Wrap(err, "tx.Commit")
	}

	return nil
}

func (db *postgresDatabase) Close() error {
	db.conn.Close()
	return nil
}

func (db *postgresDatabase) GetNumberOfTorrents() (uint, error) {
	// Using estimated number of rows which can make queries much faster
	// https://www.postgresql.org/message-id/568BF820.9060101%40comarch.com
	// https://wiki.postgresql.org/wiki/Count_estimate
	rows, err := db.conn.Query(
		context.Background(),
		"SELECT reltuples::BIGINT AS estimate_count FROM pg_class WHERE relname='torrents';",
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	if !rows.Next() {
		return 0, fmt.Errorf("no rows returned from `SELECT reltuples::BIGINT AS estimate_count`")
	}

	// Returns int64: https://godoc.org/github.com/lib/pq#hdr-Data_Types
	var n *uint
	if err = rows.Scan(&n); err != nil {
		return 0, err
	}

	// If the database is empty (i.e. 0 entries in 'torrents') then the query will return nil.
	if n == nil {
		return 0, nil
	} else {
		return *n, nil
	}
}

func (db *postgresDatabase) QueryTorrents(
	query string,
	epoch int64,
	orderBy OrderingCriteria,
	ascending bool,
	limit uint,
	lastOrderedValue *float64,
	lastID *uint64,
) ([]TorrentMetadata, error) {
	return nil, NotImplementedError
}

func (db *postgresDatabase) GetTorrent(infoHash []byte) (*TorrentMetadata, error) {
	rows, err := db.conn.Query(context.Background(), `
		SELECT
			t.info_hash,
			t.name,
			t.total_size,
			t.discovered_on,
			(SELECT COUNT(*) FROM files f WHERE f.torrent_id = t.id) AS n_files
		FROM torrents t
		WHERE t.info_hash = $1;`,
		infoHash,
	)
	if err != nil {
		return nil, err
	}

	if !rows.Next() {
		return nil, nil
	}

	var tm TorrentMetadata
	if err = rows.Scan(&tm.InfoHash, &tm.Name, &tm.Size, &tm.DiscoveredOn, &tm.NFiles); err != nil {
		return nil, err
	}

	return &tm, nil
}

func (db *postgresDatabase) GetFiles(infoHash []byte) ([]File, error) {
	rows, err := db.conn.Query(context.Background(), `
		SELECT
       		f.size,
       		f.path 
		FROM files f, torrents t WHERE f.torrent_id = t.id AND t.info_hash = $1;`,
		infoHash,
	)
	if err != nil {
		return nil, err
	}

	var files []File
	for rows.Next() {
		var file File
		if err = rows.Scan(&file.Size, &file.Path); err != nil {
			return nil, err
		}
		files = append(files, file)
	}

	return files, nil
}

func (db *postgresDatabase) GetStatistics(from string, n uint) (*Statistics, error) {
	return nil, NotImplementedError
}

func (db *postgresDatabase) setupDatabase(dsn string) error {
	sqlDB, err := sql.Open("pgx/v5", dsn)
	if err != nil {
		return errors.Wrap(err, "failed to create SQL DB instance")
	}
	defer sqlDB.Close()

	driver, err := pgx.WithInstance(sqlDB, &pgx.Config{})
	if err != nil {
		return errors.Wrap(err, "failed to create migrate driver")
	}

	srcDriver, err := httpfs.New(http.FS(migrationFiles), "postgres_migrations")
	if err != nil {
		return errors.Wrap(err, "failed to create httpfs driver")
	}

	m, err := migrate.NewWithInstance("httpfs", srcDriver, "postgres", driver)
	if err != nil {
		return errors.Wrap(err, "failed to create migration")
	}

	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		return errors.Wrap(err, "failed to apply migrations")
	}

	return nil
}
