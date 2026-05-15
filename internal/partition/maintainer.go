// Package partition automates declarative monthly partitions on the `traces`
// table. The collector process embeds a Maintainer that:
//   - on startup, ensures the current month plus N future months exist
//   - on a periodic tick (default 24h), repeats the same ensure check
//   - optionally detaches partitions older than the retention window
//
// Partitions are RANGE'd on `created_at` (BIGINT ms since epoch). Children are
// named `traces_YYYYMM` so they're trivially sortable and self-documenting.
// We DETACH, never DROP — operators can archive the detached table separately.
package partition

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	PreCreate       int
	RetentionMonths int
	CheckInterval   time.Duration
}

type Maintainer struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
	cfg    Config

	startOnce sync.Once
	cancel    context.CancelFunc
	done      chan struct{}
}

func New(pool *pgxpool.Pool, cfg Config, logger *slog.Logger) *Maintainer {
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 24 * time.Hour
	}
	if cfg.PreCreate < 0 {
		cfg.PreCreate = 0
	}
	return &Maintainer{
		pool:   pool,
		logger: logger,
		cfg:    cfg,
		done:   make(chan struct{}),
	}
}

// Start runs Ensure synchronously so the caller can guarantee partitions exist
// before consumers begin INSERTing, then launches the background tick loop.
func (m *Maintainer) Start(ctx context.Context) error {
	if err := m.Ensure(ctx); err != nil {
		return err
	}
	var started bool
	m.startOnce.Do(func() {
		runCtx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel
		go m.run(runCtx)
		started = true
	})
	if !started {
		return errors.New("partition: Start called twice")
	}
	return nil
}

func (m *Maintainer) Stop() {
	if m.cancel != nil {
		m.cancel()
		<-m.done
	}
}

// Ensure creates the partitions required to safely INSERT for the current
// month and the next PreCreate months, and detaches any partitions whose end
// boundary is older than RetentionMonths (if > 0).
func (m *Maintainer) Ensure(ctx context.Context) error {
	now := time.Now().UTC()
	for i := 0; i <= m.cfg.PreCreate; i++ {
		if err := m.createMonth(ctx, addMonths(now, i)); err != nil {
			return fmt.Errorf("partition: create month +%d: %w", i, err)
		}
	}
	if m.cfg.RetentionMonths > 0 {
		threshold := monthStart(addMonths(now, -m.cfg.RetentionMonths))
		if err := m.detachOlderThan(ctx, threshold); err != nil {
			// Detach failures aren't fatal — log and continue.
			m.logger.Warn("partition: detach older", "err", err)
		}
	}
	return nil
}

func (m *Maintainer) createMonth(ctx context.Context, ref time.Time) error {
	start := monthStart(ref)
	end := monthStart(addMonths(ref, 1))
	name := partitionName(start)
	// CREATE TABLE IF NOT EXISTS makes this idempotent so the tick loop never
	// duplicates work on a healthy cluster.
	q := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF traces FOR VALUES FROM (%d) TO (%d)`,
		name, start.UnixMilli(), end.UnixMilli(),
	)
	if _, err := m.pool.Exec(ctx, q); err != nil {
		return err
	}
	return nil
}

func (m *Maintainer) detachOlderThan(ctx context.Context, threshold time.Time) error {
	rows, err := m.pool.Query(ctx, `
		SELECT child.relname
		FROM pg_inherits i
		JOIN pg_class parent ON i.inhparent = parent.oid
		JOIN pg_class child  ON i.inhrelid  = child.oid
		WHERE parent.relname = 'traces'
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return err
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range names {
		ts, ok := parsePartitionMonth(name)
		if !ok {
			continue
		}
		// Detach when the partition's start month is strictly older than
		// the retention threshold (e.g. retention=3 and now=June means we keep
		// April/May/June and detach March or earlier).
		if !ts.Before(threshold) {
			continue
		}
		if _, err := m.pool.Exec(ctx,
			fmt.Sprintf(`ALTER TABLE traces DETACH PARTITION %s`, name)); err != nil {
			m.logger.Warn("partition: detach", "name", name, "err", err)
			continue
		}
		m.logger.Info("partition detached", "name", name, "month_start", ts.Format("2006-01"))
	}
	return nil
}

func (m *Maintainer) run(ctx context.Context) {
	defer close(m.done)
	t := time.NewTicker(m.cfg.CheckInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := m.Ensure(ctx); err != nil {
				m.logger.Error("partition: periodic ensure failed", "err", err)
			}
		}
	}
}

func partitionName(t time.Time) string {
	return "traces_" + t.UTC().Format("200601")
}

func parsePartitionMonth(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, "traces_") {
		return time.Time{}, false
	}
	suffix := strings.TrimPrefix(name, "traces_")
	ts, err := time.Parse("200601", suffix)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func addMonths(t time.Time, n int) time.Time {
	return time.Date(t.Year(), t.Month()+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
}
