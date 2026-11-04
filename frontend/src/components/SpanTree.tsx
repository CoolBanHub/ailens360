import { useState } from 'react';
import type { Trace } from '../lib/types';
import { fmtCostFine, fmtDur, fmtTs } from '../lib/fmt';
import { TokenCell } from './TokenCell';
import { CopyableId } from './CopyableId';
import { useT } from '../i18n';

export type SpanNode = Trace & {
  children: SpanNode[];
  depth: number;
  order: number;
  synthetic?: boolean;
};

export const spanKind = (s: Trace): string => s.SpanKind || 'llm';
export const isAgentSpan = (s: Trace): boolean => spanKind(s) === 'agent';
export const isToolSpan = (s: Trace): boolean => spanKind(s) === 'tool';
export const isLLMSpan = (s: Trace): boolean => spanKind(s) === 'llm';

export const agentLabel = (s: Trace): string => {
  if (s.AgentName) return s.AgentName;
  const parts = (s.AgentPath || '').split('/').filter(Boolean);
  if (parts.length > 0) return parts[parts.length - 1];
  return s.SpanName || 'agent';
};

/** Infer the role when an older client sent only agent_path. */
export const agentRoleOf = (s: Trace): string => {
  if (s.AgentRole) return s.AgentRole;
  const parts = (s.AgentPath || '').split('/').filter(Boolean);
  if (parts[0] === 'system') return 'system';
  if (parts[0] === 'main') return parts.length > 2 ? 'sub' : 'main';
  return 'system';
};

export const startMs = (s: Trace): number => new Date(s.CreatedAt).getTime() || 0;
export const endMs = (s: Trace): number => startMs(s) + (s.LatencyMs || 0);

export interface AgentRollup {
  calls: number;
  input: number;
  output: number;
  cached: number;
  cacheCreate: number;
  reasoning: number;
  cost: number;
  start: number;
  end: number;
  errors: number;
}

export function rollup(node: SpanNode): AgentRollup {
  const out: AgentRollup = {
    calls: 0, input: 0, output: 0, cached: 0, cacheCreate: 0,
    reasoning: 0, cost: 0, start: Number.POSITIVE_INFINITY, end: 0, errors: 0,
  };
  const visit = (n: SpanNode) => {
    const st = startMs(n);
    const en = endMs(n);
    if (st > 0) out.start = Math.min(out.start, st);
    out.end = Math.max(out.end, en);
    if (n.Status === 'error' || n.Status === 'aborted') out.errors++;
    if (isLLMSpan(n)) {
      out.calls++;
      out.input += n.InputTokens || 0;
      out.output += n.OutputTokens || 0;
      out.cached += n.CachedInputTokens || 0;
      out.cacheCreate += n.CacheCreationInputTokens || 0;
      out.reasoning += n.ReasoningTokens || 0;
      out.cost += n.CostUSD || 0;
    }
    n.children.forEach(visit);
  };
  visit(node);
  if (!Number.isFinite(out.start)) out.start = 0;
  return out;
}

export interface AgentSummary extends AgentRollup {
  key: string;
  name: string;
  role: string;
  path: string;
}

/** Aggregate model calls by agent path. Agent/tool spans themselves are not
 *  counted; only LLM leaves contribute tokens/cost, so the totals match the
 *  trace-level KPI tiles exactly. */
export function summarizeAgents(roots: SpanNode[]): AgentSummary[] {
  const byKey = new Map<string, AgentSummary>();
  const visit = (n: SpanNode) => {
    const hasAgent = !!(n.AgentName || n.AgentPath);
    // Agent spans create an entry even when they made no model calls, so the
    // summary count matches the tree (and an empty sub-agent is still visible).
    if (hasAgent && isAgentSpan(n)) {
      const path = n.AgentPath || (n.AgentName ? '/' + n.AgentName : '/unknown');
      if (!byKey.has(path)) {
        byKey.set(path, {
          key: path, path, name: n.AgentName || agentLabel(n), role: agentRoleOf(n),
          calls: 0, input: 0, output: 0, cached: 0, cacheCreate: 0,
          reasoning: 0, cost: 0, start: Number.POSITIVE_INFINITY, end: 0, errors: 0,
        });
      }
    }
    if (hasAgent && isLLMSpan(n)) {
      const path = n.AgentPath || '/' + n.AgentName;
      const key = path;
      let a = byKey.get(key);
      if (!a) {
        a = {
          key, path, name: n.AgentName || agentLabel(n), role: agentRoleOf(n),
          calls: 0, input: 0, output: 0, cached: 0, cacheCreate: 0,
          reasoning: 0, cost: 0, start: Number.POSITIVE_INFINITY, end: 0, errors: 0,
        };
        byKey.set(key, a);
      }
      a.calls++;
      a.input += n.InputTokens || 0;
      a.output += n.OutputTokens || 0;
      a.cached += n.CachedInputTokens || 0;
      a.cacheCreate += n.CacheCreationInputTokens || 0;
      a.reasoning += n.ReasoningTokens || 0;
      a.cost += n.CostUSD || 0;
      const st = startMs(n);
      if (st > 0) a.start = Math.min(a.start, st);
      a.end = Math.max(a.end, endMs(n));
      if (n.Status === 'error' || n.Status === 'aborted') a.errors++;
    }
    n.children.forEach(visit);
  };
  roots.forEach(visit);
  return [...byKey.values()]
    .map((a) => ({ ...a, start: Number.isFinite(a.start) ? a.start : 0 }))
    .sort((a, b) => a.start - b.start);
}

/** Build a parent/child tree from the flat trace span list. Dangling parent
 *  ids (a child was recorded but its agent span was not, e.g. header-only
 *  clients) become synthetic agent nodes so the hierarchy is still visible. */
export function buildSpanTree(items: Trace[]): { roots: SpanNode[]; byId: Map<string, SpanNode>; syntheticCount: number } {
  const byId = new Map<string, SpanNode>();
  for (const s of items) {
    byId.set(s.ID, { ...s, children: [], depth: 0, order: 0 });
  }
  const roots: SpanNode[] = [];
  const ghosts = new Map<string, SpanNode>();

  for (const s of items) {
    const node = byId.get(s.ID);
    if (!node) continue;
    const parentId = s.ParentSpanID || '';
    if (!parentId) {
      roots.push(node);
      continue;
    }
    const parent = byId.get(parentId);
    if (parent) {
      parent.children.push(node);
      continue;
    }
    let ghost = ghosts.get(parentId);
    if (!ghost) {
      ghost = {
        ...s,
        ID: parentId,
        ParentSpanID: '',
        SpanKind: 'agent',
        SpanName: s.AgentName ? `${s.AgentName}.run` : 'agent',
        Model: '',
        LatencyMs: 0,
        InputTokens: 0,
        OutputTokens: 0,
        TotalTokens: 0,
        CostUSD: 0,
        RequestBodyKey: '',
        ResponseBodyKey: '',
        children: [],
        depth: 0,
        order: 0,
        synthetic: true,
      };
      ghosts.set(parentId, ghost);
      roots.push(ghost);
    }
    ghost.children.push(node);
  }

  let order = 0;
  const sortRec = (nodes: SpanNode[], depth: number) => {
    nodes.sort((a, b) => startMs(a) - startMs(b));
    for (const n of nodes) {
      n.depth = depth;
      n.order = ++order;
      sortRec(n.children, depth + 1);
    }
  };
  sortRec(roots, 0);

  return { roots, byId, syntheticCount: ghosts.size };
}

const roleBadge = (role: string) => {
  if (role === 'main') return 'bg-indigo-50 text-indigo-700 border-indigo-200/70';
  if (role === 'sub') return 'bg-violet-50 text-violet-700 border-violet-200/70';
  if (role === 'tool') return 'bg-cyan-50 text-cyan-700 border-cyan-200/70';
  return 'bg-slate-100 text-slate-600 border-slate-200/70';
};

const roleLabelKey = (role: string) => {
  if (role === 'main') return 'detail.agent.main' as const;
  if (role === 'sub') return 'detail.agent.sub' as const;
  if (role === 'tool') return 'detail.agent.tool' as const;
  return 'detail.agent.system' as const;
};

const statusDot = (s: string) => s === 'error' ? 'err' : s === 'aborted' ? 'warn' : 'ok';

/* ── tree ─────────────────────────────────────────────────────── */

interface SpanTreeProps {
  roots: SpanNode[];
  activeSpanId: string;
  onSelect: (id: string) => void;
  t0: number;
  totalDur: number;
}

export function SpanTree({ roots, activeSpanId, onSelect, t0, totalDur }: SpanTreeProps) {
  return (
    <div className="flex flex-col gap-1.5">
      {roots.map((n) => (
        <SpanTreeNode
          key={n.ID}
          node={n}
          activeSpanId={activeSpanId}
          onSelect={onSelect}
          t0={t0}
          totalDur={totalDur}
        />
      ))}
    </div>
  );
}

function SpanTreeNode({ node, activeSpanId, onSelect, t0, totalDur }: {
  node: SpanNode;
  activeSpanId: string;
  onSelect: (id: string) => void;
  t0: number;
  totalDur: number;
}) {
  const tt = useT();
  const [open, setOpen] = useState(node.depth < 2 || node.children.length <= 4);
  const active = node.ID === activeSpanId;
  const agent = isAgentSpan(node);
  const tool = isToolSpan(node);
  const r = rollup(node);
  const start = startMs(node);
  const ownDur = Math.max(0, endMs(node) - start);
  const dur = agent ? Math.max(ownDur, r.end - r.start) : (ownDur || r.end - r.start);
  const startPct = totalDur > 0 ? Math.max(0, Math.min(100, ((start - t0) / totalDur) * 100)) : 0;
  const durPct = totalDur > 0 ? Math.max(1.5, Math.min(100 - startPct, (dur / totalDur) * 100)) : 100;
  const role = agentRoleOf(node);

  return (
    <div style={{ marginLeft: node.depth === 0 ? 0 : Math.min(36, node.depth * 12) }}>
      <button
        type="button"
        onClick={() => onSelect(node.ID)}
        className={
          'w-full text-left rounded-xl border px-3 py-2 transition relative ' +
          (active
            ? 'bg-white border-indigo-300/70 shadow-[0_2px_10px_-2px_rgba(99,102,241,0.30)]'
            : 'bg-white/55 border-white/70 hover:bg-white/85')
        }
      >
        {node.depth > 0 && (
          <div className="absolute left-[10px] top-0 bottom-0 w-px bg-[color:var(--glass-line)]" aria-hidden="true" />
        )}
        <div className="flex items-center gap-2 mb-1">
          {node.children.length > 0 ? (
            <span
              role="button"
              tabIndex={0}
              onClick={(e) => { e.stopPropagation(); setOpen((v) => !v); }}
              onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setOpen((v) => !v); } }}
              className="inline-flex h-4 w-4 items-center justify-center rounded text-ink-4 hover:text-ink-2"
              title={open ? tt('detail.agent.collapse') : tt('detail.agent.expand')}
            >
              <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                   strokeWidth="3" strokeLinecap="round" strokeLinejoin="round"
                   className={open ? 'rotate-90 transition-transform' : 'transition-transform'}>
                <path d="M9 6l6 6-6 6" />
              </svg>
            </span>
          ) : (
            <span className="w-4" />
          )}
          <span className="mono text-[10px] text-ink-4 w-5">#{node.order}</span>
          <span className={`dot ${statusDot(node.Status)}`} />
          {agent ? (
            <>
              <span className={`text-[9px] uppercase tracking-[0.12em] font-bold px-1.5 py-0.5 rounded-full border ${roleBadge(role)}`}>
                {tt(roleLabelKey(role))}
              </span>
              <span className="font-semibold text-[12.5px] text-ink truncate">{agentLabel(node)}</span>
              <span className="mono text-[10px] text-ink-4 truncate">{node.AgentPath || node.SpanName}</span>
            </>
          ) : tool ? (
            <>
              <span className="text-[9px] uppercase tracking-[0.12em] font-bold px-1.5 py-0.5 rounded-full border bg-cyan-50 text-cyan-700 border-cyan-200/70">
                {tt('detail.agent.tool')}
              </span>
              <span className="font-semibold text-[12.5px] text-ink truncate">{node.SpanName || node.Model || 'tool'}</span>
            </>
          ) : (
            <>
              <span className="font-semibold text-[12.5px] text-ink truncate">{node.Model || 'llm'}</span>
              {node.IsStream && (
                <span className="text-[8.5px] uppercase tracking-[0.14em] text-indigo-600 font-semibold px-1.5 py-0.5 rounded-full bg-indigo-50/80 border border-indigo-200/60">
                  stream
                </span>
              )}
            </>
          )}
          <span className="ml-auto text-[11px] mono text-ink-4 tnum shrink-0">{fmtDur(dur)}</span>
        </div>

        {/* waterfall */}
        <div className="relative h-1.5 rounded-full bg-slate-200/50 overflow-hidden">
          <div
            className={
              'absolute top-0 h-full rounded-full ' +
              (agent
                ? 'bg-gradient-to-r from-indigo-500 to-violet-500'
                : tool
                  ? 'bg-gradient-to-r from-cyan-500 to-teal-400'
                  : 'bg-gradient-to-r from-indigo-500 via-violet-500 to-cyan-400')
            }
            style={{ left: startPct + '%', width: durPct + '%' }}
          />
        </div>

        <div className="mt-1 flex justify-between items-center gap-2 text-[10px] mono text-ink-4">
          <span>+{fmtDur(Math.max(0, start - t0))}</span>
          <div className="flex items-center gap-2">
            {agent ? (
              <span className="tnum">{r.calls} calls · {r.input + r.output} tok</span>
            ) : tool ? (
              <span className="tnum">{node.Status || 'success'}</span>
            ) : (
              <TokenCell
                size="sm"
                tokens={{
                  input: node.InputTokens,
                  output: node.OutputTokens,
                  cached: node.CachedInputTokens,
                  cacheCreate: node.CacheCreationInputTokens,
                  reasoning: node.ReasoningTokens,
                  total: node.TotalTokens,
                  estimated: node.TokensEstimated,
                }}
              />
            )}
            {!tool && <span>{fmtCostFine(agent ? r.cost : node.CostUSD)}</span>}
          </div>
        </div>
      </button>

      {open && node.children.length > 0 && (
        <div className="mt-1.5 flex flex-col gap-1.5">
          {node.children.map((c) => (
            <SpanTreeNode
              key={c.ID}
              node={c}
              activeSpanId={activeSpanId}
              onSelect={onSelect}
              t0={t0}
              totalDur={totalDur}
            />
          ))}
        </div>
      )}
    </div>
  );
}

/* ── agent detail panel ───────────────────────────────────────── */

export function AgentSpanPanel({ node, onSelect }: { node: SpanNode; onSelect: (id: string) => void }) {
  const tt = useT();
  const r = rollup(node);
  const tool = isToolSpan(node);
  const role = tool ? 'tool' : agentRoleOf(node);
  let attrs: Record<string, unknown> | null = null;
  try {
    const parsed = JSON.parse(node.Attributes || '{}');
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) attrs = parsed as Record<string, unknown>;
  } catch { /* ignore malformed attributes */ }

  return (
    <div className="flex flex-col gap-3">
      <section className="glass glass-edge px-5 py-4">
        <div className="flex items-center gap-2 flex-wrap">
          <span className={`text-[9px] uppercase tracking-[0.12em] font-bold px-1.5 py-0.5 rounded-full border ${roleBadge(role)}`}>
            {tt(roleLabelKey(role))}
          </span>
          <h2 className="text-[17px] font-bold tracking-tight">{tool ? (node.SpanName || 'tool') : agentLabel(node)}</h2>
          <span className="mono text-[11px] text-ink-4">{node.SpanName || node.SpanKind}</span>
          <span className={`ml-auto inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[11px] font-semibold border ${
            node.Status === 'error' ? 'bg-rose-50 text-rose-700 border-rose-200/70'
              : node.Status === 'aborted' ? 'bg-amber-50 text-amber-700 border-amber-200/70'
              : 'bg-emerald-50 text-emerald-700 border-emerald-200/70'
          }`}>
            <span className={`dot ${statusDot(node.Status)}`} />
            {node.Status || 'success'}
          </span>
        </div>
        <div className="mt-3 grid grid-cols-2 md:grid-cols-4 gap-2">
          <Stat label={tool ? tt('detail.agent.toolCalls') : tt('detail.agent.calls')} value={tool ? '1' : String(r.calls)} />
          <Stat label={tt('detail.agent.duration')} value={fmtDur(tool ? node.LatencyMs : r.end - r.start)} mono />
          <Stat label={tt('detail.agent.tokens')} value={String(tool ? 0 : r.input + r.output)} mono />
          <Stat label={tt('detail.tile.cost')} value={fmtCostFine(tool ? 0 : r.cost)} mono />
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-[12px] text-ink-3">
          <span className="text-ink-4">{fmtTs(node.CreatedAt)}</span>
          {node.AgentID && <><span className="text-ink-5">·</span><span>id <span className="mono text-ink-2">{node.AgentID}</span></span></>}
          {node.AgentPath && <><span className="text-ink-5">·</span><span>path <span className="mono text-ink-2">{node.AgentPath}</span></span></>}
          {node.ParentSpanID && <><span className="text-ink-5">·</span><span>parent <CopyableId value={node.ParentSpanID} className="text-ink-2" /></span></>}
        </div>
        {node.synthetic && (
          <div className="mt-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-[11.5px] text-amber-800">
            {tt('detail.agent.syntheticHint')}
          </div>
        )}
        {node.ErrorMessage && (
          <div className="mt-3 rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-[12px] text-rose-800 break-words">
            {node.ErrorMessage}
          </div>
        )}
      </section>

      <section className="glass glass-edge p-5">
        <h3 className="text-[11px] uppercase tracking-[0.16em] text-ink-4 font-bold mb-3">
          {tt('detail.agent.children')} · {node.children.length}
        </h3>
        {node.children.length === 0 ? (
          <div className="text-xs text-ink-4">{tt('detail.agent.noChildren')}</div>
        ) : (
          <div className="flex flex-col gap-1.5">
            {node.children.map((c) => {
              const cr = rollup(c);
              return (
                <button
                  key={c.ID}
                  type="button"
                  onClick={() => onSelect(c.ID)}
                  className="w-full text-left rounded-lg border border-white/70 bg-white/55 px-3 py-2 hover:bg-white/85 transition"
                >
                  <div className="flex items-center gap-2 text-[12px]">
                    <span className={`dot ${statusDot(c.Status)}`} />
                    <span className="text-[9px] uppercase tracking-[0.12em] font-bold text-ink-4">{spanKind(c)}</span>
                    <span className="font-semibold text-ink truncate">{agentLabel(c)}</span>
                    <span className="mono text-[10px] text-ink-4 truncate">{c.SpanName || c.Model}</span>
                    <span className="ml-auto mono text-[10.5px] text-ink-4">{fmtDur(cr.end - cr.start)}</span>
                  </div>
                </button>
              );
            })}
          </div>
        )}
      </section>

      {attrs && Object.keys(attrs).length > 0 && (
        <section className="glass glass-edge p-5">
          <h3 className="text-[11px] uppercase tracking-[0.16em] text-ink-4 font-bold mb-3">attributes</h3>
          <pre className="code-line whitespace-pre-wrap break-words text-[12px]">{JSON.stringify(attrs, null, 2)}</pre>
        </section>
      )}
    </div>
  );
}

function Stat({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="rounded-2xl border border-white/70 bg-white/55 px-3.5 py-3">
      <div className="text-[10px] uppercase tracking-[0.16em] text-ink-4 font-semibold">{label}</div>
      <div className={'text-[18px] mt-0.5 leading-none ' + (mono ? 'mono' : '')}>{value}</div>
    </div>
  );
}
