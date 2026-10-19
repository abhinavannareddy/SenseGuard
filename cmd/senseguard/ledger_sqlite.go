package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

const maxLedgerConsumers = 10000

var errInvalidConsumer = errors.New("invalid consumer ID")

// BudgetLedger stores one durable balance per consumer in a SQLite database.
type BudgetLedger struct {
	mu      sync.Mutex
	db      *sql.DB
	path    string
	initial float64
}

func OpenBudgetLedger(path string, initial float64) (*BudgetLedger, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path is required")
	}
	if !finitePositive(initial) {
		initial = 5
	}

	if path != ":memory:" {
		parent := filepath.Dir(path)
		if parent != "." && parent != "" {
			if err := os.MkdirAll(parent, 0700); err != nil {
				return nil, fmt.Errorf("create database directory: %w", err)
			}
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, fmt.Errorf("open database file: %w", err)
		}
		if err := file.Close(); err != nil {
			return nil, fmt.Errorf("close database file: %w", err)
		}
		// Windows ACLs control file access; POSIX mode bits are applied on Unix.
		if runtime.GOOS != "windows" {
			if err := os.Chmod(path, 0600); err != nil {
				return nil, fmt.Errorf("restrict database file permissions: %w", err)
			}
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ledger := &BudgetLedger{db: db, path: path, initial: initial}
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure SQLite busy timeout: %w", err)
	}
	const schema = `CREATE TABLE IF NOT EXISTS privacy_budgets (
		consumer_id TEXT PRIMARY KEY NOT NULL CHECK (length(consumer_id) BETWEEN 1 AND 128),
		remaining REAL NOT NULL CHECK (remaining >= 0),
		updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize privacy budget schema: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("check SQLite database: %w", err)
	}
	return ledger, nil
}

// BackupTo creates a consistent SQLite snapshot at a new path without
// overwriting an existing file. The destination directory must already exist.
func (b *BudgetLedger) BackupTo(ctx context.Context, destination string) error {
	if strings.TrimSpace(destination) == "" || destination != strings.TrimSpace(destination) {
		return errors.New("backup destination is invalid")
	}
	sourcePath, err := filepath.Abs(b.path)
	if err != nil {
		return fmt.Errorf("resolve database path: %w", err)
	}
	destinationPath, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("resolve backup path: %w", err)
	}
	samePath := filepath.Clean(sourcePath) == filepath.Clean(destinationPath)
	if runtime.GOOS == "windows" {
		samePath = strings.EqualFold(filepath.Clean(sourcePath), filepath.Clean(destinationPath))
	}
	if samePath {
		return errors.New("backup destination must differ from the source database")
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.db == nil {
		return errors.New("database is closed")
	}
	file, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("create backup without overwriting: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := file.Chmod(0600); err != nil {
			_ = file.Close()
			_ = os.Remove(destinationPath)
			return fmt.Errorf("restrict backup file permissions: %w", err)
		}
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(destinationPath)
		return fmt.Errorf("close new backup file: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(destinationPath)
		}
	}()
	if _, err := b.db.ExecContext(ctx, "VACUUM INTO ?", destinationPath); err != nil {
		return fmt.Errorf("create SQLite snapshot: %w", err)
	}
	if err := verifySQLiteDatabase(destinationPath); err != nil {
		return fmt.Errorf("verify SQLite snapshot: %w", err)
	}
	complete = true
	return nil
}

// verifySQLiteDatabase checks database integrity and the expected ledger table.
func verifySQLiteDatabase(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat database: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("database file is not a non-empty regular file")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	var integrity string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
		return fmt.Errorf("run SQLite integrity check: %w", err)
	}
	if integrity != "ok" {
		return errors.New("SQLite integrity check failed")
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM privacy_budgets").Scan(&count); err != nil {
		return fmt.Errorf("verify privacy budget table: %w", err)
	}
	return nil
}

func (b *BudgetLedger) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.db == nil {
		return nil
	}
	err := b.db.Close()
	b.db = nil
	return err
}

func (b *BudgetLedger) Ping(ctx context.Context) error {
	if !b.mu.TryLock() {
		return errors.New("budget database is busy")
	}
	defer b.mu.Unlock()
	if b.db == nil {
		return errors.New("database is closed")
	}
	return b.db.PingContext(ctx)
}

// Charge atomically initializes a consumer if needed and deducts one request's cost.
func (b *BudgetLedger) Charge(consumer string, cost float64) (remaining float64, charged bool, full bool, err error) {
	if consumer == "" || len(consumer) > 128 {
		return 0, false, false, errInvalidConsumer
	}
	if !finitePositive(cost) {
		return 0, false, false, errors.New("request cost must be finite and positive")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.db == nil {
		return 0, false, false, errors.New("database is closed")
	}
	tx, err := b.db.BeginTx(context.Background(), nil)
	if err != nil {
		return 0, false, false, fmt.Errorf("begin budget transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// This single insert statement checks the global cap and creates new balances
	// only once. SQLite serializes writers, and the transaction keeps the later
	// read, deduction, and commit together.
	_, err = tx.Exec(`INSERT INTO privacy_budgets (consumer_id, remaining)
		SELECT ?, ? WHERE (SELECT COUNT(*) FROM privacy_budgets) < ?
		ON CONFLICT (consumer_id) DO NOTHING`, consumer, b.initial, maxLedgerConsumers)
	if err != nil {
		return 0, false, false, fmt.Errorf("initialize consumer budget: %w", err)
	}

	var current float64
	err = tx.QueryRow("SELECT remaining FROM privacy_budgets WHERE consumer_id = ?", consumer).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, true, nil
	}
	if err != nil {
		return 0, false, false, fmt.Errorf("read consumer budget: %w", err)
	}
	if math.IsNaN(current) || math.IsInf(current, 0) || current < 0 {
		return 0, false, false, errors.New("stored privacy budget is invalid")
	}
	if current < cost {
		return current, false, false, nil
	}
	next := current - cost
	if next >= current || math.IsNaN(next) || math.IsInf(next, 0) || next < 0 {
		return current, false, false, nil
	}
	result, err := tx.Exec(`UPDATE privacy_budgets
		SET remaining = ?, updated_at = CURRENT_TIMESTAMP
		WHERE consumer_id = ? AND remaining = ?`, next, consumer, current)
	if err != nil {
		return 0, false, false, fmt.Errorf("deduct privacy budget: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, false, false, fmt.Errorf("confirm privacy budget deduction: %w", err)
	}
	if rows != 1 {
		return 0, false, false, errors.New("privacy budget deduction was not confirmed")
	}
	if err := tx.Commit(); err != nil {
		return 0, false, false, fmt.Errorf("commit privacy budget deduction: %w", err)
	}
	return next, true, false, nil
}

// Snapshot reads a balance without creating a new consumer record.
func (b *BudgetLedger) Snapshot(consumer string) (remaining float64, full bool, err error) {
	if consumer == "" || len(consumer) > 128 {
		return 0, false, errInvalidConsumer
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.snapshotLocked(consumer)
}

func (b *BudgetLedger) snapshotLocked(consumer string) (remaining float64, full bool, err error) {
	if b.db == nil {
		return 0, false, errors.New("database is closed")
	}
	err = b.db.QueryRow("SELECT remaining FROM privacy_budgets WHERE consumer_id = ?", consumer).Scan(&remaining)
	if err == nil {
		if math.IsNaN(remaining) || math.IsInf(remaining, 0) || remaining < 0 {
			return 0, false, errors.New("stored privacy budget is invalid")
		}
		return remaining, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, fmt.Errorf("read privacy budget: %w", err)
	}
	var count int
	if err := b.db.QueryRow("SELECT COUNT(*) FROM privacy_budgets").Scan(&count); err != nil {
		return 0, false, fmt.Errorf("count privacy budgets: %w", err)
	}
	if count >= maxLedgerConsumers {
		return 0, true, nil
	}
	return b.initial, false, nil
}
