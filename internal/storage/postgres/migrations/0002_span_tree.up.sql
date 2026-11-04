-- Span tree + agent identity.
--
-- Until now `traces` was a flat set of LLM HTTP calls grouped by trace_id.
-- Agent workloads need a real tree: an agent/run span is the root, LLM calls
-- and tool calls hang off the agent that caused them, and sub-agents are
-- interior nodes. These columns are additive; legacy rows keep SpanKind='llm'
-- and ParentSpanID='' so the existing list/detail UI still works.
ALTER TABLE traces
    ADD COLUMN IF NOT EXISTS parent_span_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS span_kind      TEXT NOT NULL DEFAULT 'llm',
    ADD COLUMN IF NOT EXISTS span_name      TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS agent_id       TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS agent_name     TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS agent_role     TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS agent_path     TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS attributes     TEXT NOT NULL DEFAULT '{}';

-- Tree reads fetch one trace_id and link children by parent_span_id.
CREATE INDEX IF NOT EXISTS idx_traces_trace_parent ON traces (trace_id, parent_span_id);
CREATE INDEX IF NOT EXISTS idx_traces_trace_kind   ON traces (trace_id, span_kind);
CREATE INDEX IF NOT EXISTS idx_traces_agent        ON traces (project_id, agent_name, created_at DESC);

-- Billing/usage must only count model calls. Keeping a view makes the
-- "filter out agent/tool spans" rule explicit and hard to forget when a new
-- aggregation query is added.
CREATE OR REPLACE VIEW llm_spans AS
    SELECT * FROM traces WHERE span_kind = 'llm';
