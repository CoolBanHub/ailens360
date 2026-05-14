-- Consolidated schema for the Postgres baseline. Timestamps are stored as
-- BIGINT (Unix seconds for projects; Unix milliseconds for traces) to match
-- the application layer, keep dimension bucketing simple (division), and
-- avoid driver-level time-zone surprises.

CREATE TABLE IF NOT EXISTS projects (
    id          TEXT PRIMARY KEY,
    project_key TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL UNIQUE,
    created_at  BIGINT NOT NULL,
    updated_at  BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_projects_project_key ON projects(project_key);

CREATE TABLE IF NOT EXISTS traces (
    id                 TEXT PRIMARY KEY,
    trace_id           TEXT NOT NULL DEFAULT '',
    trace_name         TEXT NOT NULL DEFAULT '',
    project_id         TEXT NOT NULL,
    user_id            TEXT NOT NULL DEFAULT '',
    session_id         TEXT NOT NULL DEFAULT '',
    tags               TEXT NOT NULL DEFAULT '',
    provider           TEXT NOT NULL,
    model              TEXT NOT NULL,
    is_stream          BOOLEAN NOT NULL DEFAULT FALSE,
    status             TEXT NOT NULL,
    status_code        INTEGER NOT NULL,
    error_message      TEXT NOT NULL DEFAULT '',
    request_headers    TEXT NOT NULL DEFAULT '',
    request_body       TEXT NOT NULL DEFAULT '',
    request_path       TEXT NOT NULL DEFAULT '',
    response_headers   TEXT NOT NULL DEFAULT '',
    response_body      TEXT NOT NULL DEFAULT '',
    stream_chunks      TEXT NOT NULL DEFAULT '',
    timeline           TEXT NOT NULL DEFAULT '',
    input_tokens                INTEGER NOT NULL DEFAULT 0,
    output_tokens               INTEGER NOT NULL DEFAULT 0,
    total_tokens                INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens            INTEGER NOT NULL DEFAULT 0, -- thinking/reasoning (OpenAI o-series, Gemini thoughts)
    cached_input_tokens         INTEGER NOT NULL DEFAULT 0, -- input served from cache at discounted rate
    cache_creation_input_tokens INTEGER NOT NULL DEFAULT 0, -- Anthropic-only cache-write billing
    tokens_estimated            BOOLEAN NOT NULL DEFAULT FALSE,
    cost_usd           DOUBLE PRECISION NOT NULL DEFAULT 0,
    latency_ms         BIGINT NOT NULL DEFAULT 0,
    ttft_ms            BIGINT,
    ttfb_ms            BIGINT,
    gen_duration_ms    BIGINT,
    tps                DOUBLE PRECISION NOT NULL DEFAULT 0,
    chunk_count        INTEGER NOT NULL DEFAULT 0,
    bytes_streamed     BIGINT NOT NULL DEFAULT 0,
    finish_reason      TEXT NOT NULL DEFAULT '',
    stream_status      TEXT NOT NULL DEFAULT '',
    created_at         BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_traces_project_created  ON traces(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_traces_provider         ON traces(provider, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_traces_model            ON traces(model, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_traces_status           ON traces(status);
CREATE INDEX IF NOT EXISTS idx_traces_user_created     ON traces(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_traces_session_created  ON traces(session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_traces_trace_id_created ON traces(trace_id, created_at ASC);
