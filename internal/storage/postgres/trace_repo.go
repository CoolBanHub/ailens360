package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/CoolBanHub/ailens360/internal/storage/repo"
)

type TraceRepo struct{ pool *pgxpool.Pool }

func NewTraceRepo(pool *pgxpool.Pool) *TraceRepo { return &TraceRepo{pool: pool} }

// traceCols intentionally lists columns in the order required by INSERT/SELECT
// and CopyFrom — keep it in sync with traceCopyCols below.
const traceCols = `id, trace_id, trace_name, project_id, user_id, session_id, tags, model, is_stream, status, status_code, error_message,
		request_headers, request_path, response_headers, request_body_key, response_body_key, request_body_size, response_body_size, timeline,
		input_tokens, output_tokens, total_tokens, reasoning_tokens, cached_input_tokens, cache_creation_input_tokens, tokens_estimated, cost_usd, latency_ms,
		ttft_ms, ttfb_ms, gen_duration_ms, tps, chunk_count, bytes_streamed, finish_reason, stream_status, created_at`

var traceCopyCols = []string{
	"id", "trace_id", "trace_name", "project_id", "user_id", "session_id", "tags", "model", "is_stream",
	"status", "status_code", "error_message",
	"request_headers", "request_path", "response_headers",
	"request_body_key", "response_body_key", "request_body_size", "response_body_size", "timeline",
	"input_tokens", "output_tokens", "total_tokens", "reasoning_tokens", "cached_input_tokens", "cache_creation_input_tokens",
	"tokens_estimated", "cost_usd", "latency_ms",
	"ttft_ms", "ttfb_ms", "gen_duration_ms", "tps", "chunk_count", "bytes_streamed", "finish_reason", "stream_status", "created_at",
}

func (r *TraceRepo) Create(ctx context.Context, t *repo.Trace) error {
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO traces(`+traceCols+`)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12, $13,$14,$15,$16,$17,$18,$19,$20, $21,$22,$23,$24,$25,$26,$27,$28,$29, $30,$31,$32,$33,$34,$35,$36,$37, $38)`,
		t.ID, t.TraceID, t.TraceName, t.ProjectID, t.UserID, t.SessionID, t.Tags, t.Model, t.IsStream,
		t.Status, t.StatusCode, t.ErrorMessage,
		t.RequestHeaders, t.RequestPath, t.ResponseHeaders,
		t.RequestBodyKey, t.ResponseBodyKey, t.RequestBodySize, t.ResponseBodySize, t.Timeline,
		t.InputTokens, t.OutputTokens, t.TotalTokens, t.ReasoningTokens, t.CachedInputTokens, t.CacheCreationInputTokens,
		t.TokensEstimated, t.CostUSD, t.LatencyMs,
		nullableInt64(t.TTFTMs), nullableInt64(t.TTFBMs), nullableInt64(t.GenDurationMs),
		t.TPS, t.ChunkCount, t.BytesStreamed, t.FinishReason, t.StreamStatus,
		t.CreatedAt.UnixMilli(),
	)
	return err
}

// BatchCreate uses COPY for high throughput on hot ingest paths.
func (r *TraceRepo) BatchCreate(ctx context.Context, ts []*repo.Trace) error {
	if len(ts) == 0 {
		return nil
	}
	rows := make([][]any, 0, len(ts))
	for _, t := range ts {
		if t.CreatedAt.IsZero() {
			t.CreatedAt = time.Now()
		}
		rows = append(rows, []any{
			t.ID, t.TraceID, t.TraceName, t.ProjectID, t.UserID, t.SessionID, t.Tags, t.Model, t.IsStream,
			t.Status, t.StatusCode, t.ErrorMessage,
			t.RequestHeaders, t.RequestPath, t.ResponseHeaders,
			t.RequestBodyKey, t.ResponseBodyKey, t.RequestBodySize, t.ResponseBodySize, t.Timeline,
			t.InputTokens, t.OutputTokens, t.TotalTokens, t.ReasoningTokens, t.CachedInputTokens, t.CacheCreationInputTokens,
			t.TokensEstimated, t.CostUSD, t.LatencyMs,
			nullableInt64(t.TTFTMs), nullableInt64(t.TTFBMs), nullableInt64(t.GenDurationMs),
			t.TPS, t.ChunkCount, t.BytesStreamed, t.FinishReason, t.StreamStatus,
			t.CreatedAt.UnixMilli(),
		})
	}
	_, err := r.pool.CopyFrom(ctx, pgx.Identifier{"traces"}, traceCopyCols, pgx.CopyFromRows(rows))
	return err
}

func nullableInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func (r *TraceRepo) GetByID(ctx context.Context, id string) (*repo.Trace, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+traceCols+` FROM traces WHERE id=$1`, id)
	return scanTrace(row)
}

// pgPlaceholderRewriter rewrites a `?`-style query into Postgres `$N` style.
// We use `?` in builders that compose dynamic WHERE clauses (the rest of the
// queries are static $1/$2/... already). Caveat: do not use `?` inside string
// literals in dynamic clauses — none of the call sites here do.
func toPg(q string) string {
	var b strings.Builder
	b.Grow(len(q) + 8)
	n := 0
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
			continue
		}
		b.WriteByte(q[i])
	}
	return b.String()
}

func (r *TraceRepo) List(ctx context.Context, f repo.ListTraceFilter) ([]*repo.Trace, int64, error) {
	var (
		where []string
		args  []any
	)
	if f.ProjectID != "" {
		where = append(where, "project_id=?")
		args = append(args, f.ProjectID)
	}
	if f.TraceID != "" {
		where = append(where, "trace_id=?")
		args = append(args, f.TraceID)
	}
	if f.UserID != "" {
		where = append(where, "user_id=?")
		args = append(args, f.UserID)
	}
	if f.SessionID != "" {
		where = append(where, "session_id=?")
		args = append(args, f.SessionID)
	}
	if f.Model != "" {
		where = append(where, "model=?")
		args = append(args, f.Model)
	}
	if f.Status != "" {
		where = append(where, "status=?")
		args = append(args, f.Status)
	}
	if f.StartUnixMs > 0 {
		where = append(where, "created_at>=?")
		args = append(args, f.StartUnixMs)
	}
	if f.EndUnixMs > 0 {
		where = append(where, "created_at<=?")
		args = append(args, f.EndUnixMs)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}

	var total int64
	if err := r.pool.QueryRow(ctx,
		toPg("SELECT COUNT(1) FROM traces"+clause), args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	order := "DESC"
	if f.TraceID != "" {
		order = "ASC"
	}
	q := "SELECT " + traceCols + " FROM traces" + clause +
		" ORDER BY created_at " + order + " LIMIT ? OFFSET ?"
	args = append(args, limit, f.Offset)
	rows, err := r.pool.Query(ctx, toPg(q), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*repo.Trace
	for rows.Next() {
		t, err := scanTrace(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
}

func (r *TraceRepo) ListGroups(ctx context.Context, f repo.ListTraceGroupFilter) ([]*repo.TraceGroup, int64, error) {
	var (
		where []string
		args  []any
	)
	if f.ProjectID != "" {
		where = append(where, "project_id=?")
		args = append(args, f.ProjectID)
	}
	if f.UserID != "" {
		where = append(where, "user_id=?")
		args = append(args, f.UserID)
	}
	if f.SessionID != "" {
		where = append(where, "session_id=?")
		args = append(args, f.SessionID)
	}
	if f.TraceName != "" {
		where = append(where, "trace_name=?")
		args = append(args, f.TraceName)
	}
	if f.Model != "" {
		where = append(where, "model=?")
		args = append(args, f.Model)
	}
	if f.StartUnixMs > 0 {
		where = append(where, "created_at>=?")
		args = append(args, f.StartUnixMs)
	}
	if f.EndUnixMs > 0 {
		where = append(where, "created_at<=?")
		args = append(args, f.EndUnixMs)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}

	// In Postgres NULLIF + COALESCE work identically to SQLite; trace_id is
	// NOT NULL with default '', so NULLIF folds empties back to the span id.
	const gidExpr = "COALESCE(NULLIF(trace_id, ''), id)"

	const statusCase = "CASE " +
		"WHEN MAX(CASE WHEN status='error' THEN 3 WHEN status='aborted' THEN 2 ELSE 1 END)=3 THEN 'error' " +
		"WHEN MAX(CASE WHEN status='error' THEN 3 WHEN status='aborted' THEN 2 ELSE 1 END)=2 THEN 'aborted' " +
		"ELSE 'success' END"
	having := ""
	var havingArgs []any
	if f.Status != "" {
		having = " HAVING " + statusCase + " = ?"
		havingArgs = append(havingArgs, f.Status)
	}

	var total int64
	countQ := "SELECT COUNT(*) FROM (SELECT " + gidExpr + " AS gid FROM traces" + clause +
		" GROUP BY gid" + having + ") AS g"
	countArgs := append(append([]any{}, args...), havingArgs...)
	if err := r.pool.QueryRow(ctx, toPg(countQ), countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}

	q := `SELECT
		` + gidExpr + ` AS gid,
		MAX(trace_name) AS trace_name,
		MAX(project_id),
		MAX(user_id),
		MAX(session_id),
		COUNT(1) AS span_count,
		COALESCE(SUM(input_tokens),0),
		COALESCE(SUM(output_tokens),0),
		COALESCE(SUM(total_tokens),0),
		COALESCE(SUM(reasoning_tokens),0),
		COALESCE(SUM(cached_input_tokens),0),
		COALESCE(SUM(cache_creation_input_tokens),0),
		COALESCE(SUM(cost_usd),0),
		(MAX(created_at) - MIN(created_at)) + COALESCE(MAX(latency_ms),0) AS latency_ms,
		` + statusCase + ` AS status,
		MIN(created_at) AS started_at
		FROM traces` + clause + `
		GROUP BY gid` + having + `
		ORDER BY started_at DESC
		LIMIT ? OFFSET ?`
	listArgs := append(append([]any{}, args...), havingArgs...)
	listArgs = append(listArgs, limit, f.Offset)
	rows, err := r.pool.Query(ctx, toPg(q), listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*repo.TraceGroup
	for rows.Next() {
		var g repo.TraceGroup
		var startedMs int64
		if err := rows.Scan(&g.TraceID, &g.TraceName, &g.ProjectID, &g.UserID, &g.SessionID,
			&g.SpanCount, &g.InputTokens, &g.OutputTokens, &g.TotalTokens,
			&g.ReasoningTokens, &g.CachedInputTokens, &g.CacheCreationInputTokens,
			&g.CostUSD, &g.LatencyMs, &g.Status, &startedMs); err != nil {
			return nil, 0, err
		}
		g.StartedAt = time.UnixMilli(startedMs)
		out = append(out, &g)
	}
	return out, total, rows.Err()
}

func (r *TraceRepo) UsageByDimension(ctx context.Context, dim string, startMs, endMs int64, projectID string) ([]repo.UsageStat, error) {
	keyCol := ""
	switch dim {
	case "model":
		keyCol = "model"
	case "project":
		keyCol = "project_id"
	case "day":
		keyCol = "(created_at / 86400000)"
	case "hour":
		keyCol = "(created_at / 3600000)"
	default:
		return nil, fmt.Errorf("unsupported dimension: %s", dim)
	}
	var where []string
	var args []any
	if startMs > 0 {
		where = append(where, "created_at>=?")
		args = append(args, startMs)
	}
	if endMs > 0 {
		where = append(where, "created_at<=?")
		args = append(args, endMs)
	}
	if projectID != "" {
		where = append(where, "project_id=?")
		args = append(args, projectID)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	// Postgres requires explicit cast for booleans in AVG over CASE.
	q := fmt.Sprintf(`SELECT %s::TEXT AS k,
		COUNT(1),
		COALESCE(SUM(input_tokens),0),
		COALESCE(SUM(output_tokens),0),
		COALESCE(SUM(total_tokens),0),
		COALESCE(SUM(reasoning_tokens),0),
		COALESCE(SUM(cached_input_tokens),0),
		COALESCE(SUM(cache_creation_input_tokens),0),
		COALESCE(SUM(cost_usd),0),
		COALESCE(AVG(latency_ms),0),
		COALESCE(AVG(CASE WHEN status='error' THEN 1.0 ELSE 0.0 END),0)
		FROM traces%s GROUP BY k ORDER BY k`, keyCol, clause)
	rows, err := r.pool.Query(ctx, toPg(q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []repo.UsageStat
	for rows.Next() {
		var u repo.UsageStat
		if err := rows.Scan(&u.Key, &u.Calls, &u.InputTokens, &u.OutputTokens,
			&u.TotalTokens, &u.ReasoningTokens, &u.CachedInputTokens, &u.CacheCreationInputTokens,
			&u.CostUSD, &u.AvgLatencyMs, &u.ErrorRate); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (r *TraceRepo) Facets(ctx context.Context, projectID string) (models []string, hasAny bool, err error) {
	models, err = distinctNonEmpty(ctx, r.pool, "model", projectID)
	if err != nil {
		return nil, false, err
	}
	// Fast existence check independent of the model column — covers traces
	// with an empty / unparseable model that wouldn't show up in `models`.
	if err = r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM traces WHERE project_id=$1)`, projectID).Scan(&hasAny); err != nil {
		return nil, false, err
	}
	return models, hasAny, nil
}

// distinctNonEmpty returns the distinct non-empty values of `col` for a given
// project, ordered by frequency desc then alpha. The column name is whitelisted
// by the caller — never pass user input here.
func distinctNonEmpty(ctx context.Context, pool *pgxpool.Pool, col, projectID string) ([]string, error) {
	q := fmt.Sprintf(`SELECT %s FROM traces
		WHERE project_id = $1 AND %s <> ''
		GROUP BY %s
		ORDER BY COUNT(*) DESC, %s ASC
		LIMIT 200`, col, col, col, col)
	rows, err := pool.Query(ctx, q, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0, 8)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func scanTrace(s rowScanner) (*repo.Trace, error) {
	var t repo.Trace
	var ttft, ttfb, gen *int64
	var createdMs int64
	if err := s.Scan(&t.ID, &t.TraceID, &t.TraceName, &t.ProjectID, &t.UserID, &t.SessionID, &t.Tags, &t.Model,
		&t.IsStream, &t.Status, &t.StatusCode, &t.ErrorMessage,
		&t.RequestHeaders, &t.RequestPath, &t.ResponseHeaders,
		&t.RequestBodyKey, &t.ResponseBodyKey, &t.RequestBodySize, &t.ResponseBodySize, &t.Timeline,
		&t.InputTokens, &t.OutputTokens, &t.TotalTokens, &t.ReasoningTokens, &t.CachedInputTokens, &t.CacheCreationInputTokens,
		&t.TokensEstimated, &t.CostUSD, &t.LatencyMs,
		&ttft, &ttfb, &gen, &t.TPS, &t.ChunkCount, &t.BytesStreamed, &t.FinishReason, &t.StreamStatus,
		&createdMs); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repo.ErrNotFound
		}
		return nil, err
	}
	t.TTFTMs = ttft
	t.TTFBMs = ttfb
	t.GenDurationMs = gen
	t.CreatedAt = time.UnixMilli(createdMs)
	return &t, nil
}
