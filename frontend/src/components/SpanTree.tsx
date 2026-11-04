import { useMemo, useState } from 'react';
import type { Trace } from '../lib/types';
import { fmtCostFine, fmtDur, fmtTokens, fmtTs } from '../lib/fmt';
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

export interface AgentGroup extends AgentRollup {
  key: string;
  name: string;
  role: string;
  path: string;
  agent: SpanNode;
  spans: Trace[];
  callSpans: Trace[];
  toolSpans: Trace[];
}

export interface AgentSummary extends AgentRollup {
  key: string;
  name: string;
  role: string;
  path: string;
}

function rollupSpans(spans: Trace[]): AgentRollup {
  const out: AgentRollup = {
    calls: 0, input: 0, output: 0, cached: 0, cacheCreate: 0,
    reasoning: 0, cost: 0, start: Number.POSITIVE_INFINITY, end: 0, errors: 0,
  };
  for (const s of spans) {
    const st = startMs(s);
    const en = endMs(s);
    if (st > 0) out.start = Math.min(out.start, st);
    out.end = Math.max(out.end, en);
    if (s.Status === 'error' || s.Status === 'aborted') out.errors++;
    if (isLLMSpan(s)) {
      out.calls++;
      out.input += s.InputTokens || 0;
      out.output += s.OutputTokens || 0;
      out.cached += s.CachedInputTokens || 0;
      out.cacheCreate += s.CacheCreationInputTokens || 0;
      out.reasoning += s.ReasoningTokens || 0;
      out.cost += s.CostUSD || 0;
    }
  }
  if (!Number.isFinite(out.start)) out.start = 0;
  return out;
}

/**
 * Build flat agent sections instead of a deep tree. Agents are grouped by
 * agent_path, so a trace with router + worker + memory reads as three clear
 * cards rather than an indented call tree. Nested agents still show their
 * path; the UI does not need extra indentation to explain ownership.
 */
export function buildAgentGroups(items: Trace[]): {
  groups: AgentGroup[];
  byId: Map<string, SpanNode>;
  hasAgentMeta: boolean;
} {
  const byId = new Map<string, SpanNode>();
  for (const s of items) {
    byId.set(s.ID, { ...s, children: [], depth: 0, order: 0 });
  }
  for (const s of items) {
    const node = byId.get(s.ID);
    const parent = s.ParentSpanID ? byId.get(s.ParentSpanID) : undefined;
    if (node && parent) parent.children.push(node);
  }

  const hasAgentMeta = items.some((s) => !!(s.AgentName || s.AgentPath));
  const buckets = new Map<string, Trace[]>();
  for (const s of items) {
    const path = s.AgentPath || (s.AgentName ? '/' + s.AgentName : '');
    const key = path || '__unknown__';
    if (!buckets.has(key)) buckets.set(key, []);
    buckets.get(key)!.push(s);
  }

  const groups: AgentGroup[] = [];
  for (const [key, spans] of buckets) {
    spans.sort((a, b) => startMs(a) - startMs(b));
    const first = spans[0];
    const path = key === '__unknown__' ? '' : key;
    const name = first.AgentName || (path ? path.split('/').filter(Boolean).pop() || 'agent' : '调用');
    const role = hasAgentMeta ? agentRoleOf(first) : '';

    const realAgent = spans.find(isAgentSpan);
    let agent = realAgent ? byId.get(realAgent.ID) : undefined;
    if (!agent) {
      const synthetic: SpanNode = {
        ...first,
        ID: 'synthetic:' + (path || 'unknown'),
        ParentSpanID: '',
        SpanKind: 'agent',
        SpanName: name ? `${name}.run` : 'agent',
        Model: '',
        AgentName: path ? name : '',
        AgentRole: role,
        AgentPath: path,
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
      byId.set(synthetic.ID, synthetic);
      agent = synthetic;
    }

    // AgentSpanPanel lists direct leaves; keep the real children plus every
    // span that belongs to this agent path.
    const childIds = new Set(agent.children.map((c) => c.ID));
    for (const s of spans) {
      const n = byId.get(s.ID);
      if (n && n.ID !== agent.ID && !childIds.has(n.ID)) {
        agent.children.push(n);
        childIds.add(n.ID);
      }
    }

    const callSpans = spans.filter(isLLMSpan).sort((a, b) => startMs(a) - startMs(b));
    const toolSpans = spans.filter(isToolSpan).sort((a, b) => startMs(a) - startMs(b));
    const roll = rollupSpans(spans);
    groups.push({
      key, name, role, path, agent, spans, callSpans, toolSpans, ...roll,
    });
  }

  const rank = (role: string) => role === 'main' ? 0 : role === 'sub' ? 1 : role === 'system' ? 2 : 3;
  groups.sort((a, b) => rank(a.role) - rank(b.role) || a.start - b.start);
  return { groups, byId, hasAgentMeta };
}

export function summarizeGroups(groups: AgentGroup[]): AgentSummary[] {
  return groups
    .filter((g) => !!g.path)
    .map((g) => ({
      key: g.key,
      name: g.name,
      role: g.role,
      path: g.path,
      calls: g.calls,
      input: g.input,
      output: g.output,
      cached: g.cached,
      cacheCreate: g.cacheCreate,
      reasoning: g.reasoning,
      cost: g.cost,
      start: g.start,
      end: g.end,
      errors: g.errors,
    }));
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

const accentClass = (role: string) => {
  if (role === 'main') return 'border-l-indigo-500';
  if (role === 'sub') return 'border-l-violet-500';
  if (role === 'tool') return 'border-l-cyan-500';
  return 'border-l-slate-400';
};

/* ── grouped agent list ──────────────────────────────────────── */

interface AgentGroupsProps {
  groups: AgentGroup[];
  hasAgentMeta: boolean;
  activeSpanId: string;
  onSelect: (id: string) => void;
  t0: number;
}

export function AgentGroups({ groups, hasAgentMeta, activeSpanId, onSelect, t0 }: AgentGroupsProps) {
  const tt = useT();
  const legacy = useMemo(() => {
    if (hasAgentMeta) return [];
    return groups
      .flatMap((g) => g.spans)
      .filter((s) => isLLMSpan(s) || isToolSpan(s))
      .sort((a, b) => startMs(a) - startMs(b));
  }, [groups, hasAgentMeta]);

  if (!hasAgentMeta) {
    return (
      <div className="rounded-xl border border-white/70 bg-white/45 overflow-hidden">
        {legacy.length === 0 ? (
          <div className="px-3 py-4 text-xs text-ink-4">{tt('detail.spans.empty')}</div>
        ) : legacy.map((s, i) => (
          isToolSpan(s)
            ? <ToolRow key={s.ID} span={s} active={s.ID === activeSpanId} onSelect={onSelect} t0={t0} />
            : <CallRow key={s.ID} span={s} index={i + 1} active={s.ID === activeSpanId} onSelect={onSelect} t0={t0} />
        ))}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-2.5">
      {groups.map((g) => (
        <AgentGroupCard
          key={g.key}
          group={g}
          activeSpanId={activeSpanId}
          onSelect={onSelect}
          t0={t0}
        />
      ))}
    </div>
  );
}

function AgentGroupCard({ group, activeSpanId, onSelect, t0 }: {
  group: AgentGroup;
  activeSpanId: string;
  onSelect: (id: string) => void;
  t0: number;
}) {
  const tt = useT();
  const [open, setOpen] = useState(true);
  const isActive = group.agent.ID === activeSpanId;
  const hasActiveChild = group.callSpans.some((c) => c.ID === activeSpanId) || group.toolSpans.some((c) => c.ID === activeSpanId);
  const tokens = group.input + group.output;
  const role = group.role || '';
  const offset = Math.max(0, group.start - t0);

  return (
    <section
      className={
        'rounded-xl border border-white/70 border-l-4 bg-white/45 overflow-hidden transition ' +
        accentClass(role) + ' ' +
        (isActive ? 'bg-white shadow-[0_2px_10px_-2px_rgba(99,102,241,0.25)]' : '')
      }
    >
      <button
        type="button"
        onClick={() => onSelect(group.agent.ID)}
        className="w-full text-left px-3 py-2.5 hover:bg-white/60 transition"
      >
        <div className="flex items-start gap-2">
          <span className={`shrink-0 whitespace-nowrap text-[9px] uppercase tracking-[0.10em] font-bold px-1.5 py-0.5 rounded-full border ${roleBadge(role)}`}>
            {role ? tt(roleLabelKey(role)) : tt('detail.agent.unknown')}
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex items-baseline gap-1.5 min-w-0">
              <span className="font-bold text-[13.5px] text-ink truncate" title={group.name}>{group.name}</span>
              {group.path && (
                <span className="mono text-[10px] text-ink-4 truncate" title={group.path}>{group.path}</span>
              )}
            </div>
            <div className="mt-1 flex items-center gap-x-2 gap-y-0.5 flex-wrap text-[10px] mono text-ink-4 tnum">
              <span>+{fmtDur(offset)}</span>
              <span className="text-ink-5">·</span>
              <span>{group.callSpans.length} calls</span>
              <span className="text-ink-5">·</span>
              <span>{fmtTokens(tokens)} tok</span>
              <span className="text-ink-5">·</span>
              <span>{fmtCostFine(group.cost)}</span>
            </div>
          </div>
          <div className="shrink-0 flex items-center gap-1.5">
            <span className="mono text-[11px] text-ink-3 tnum">{fmtDur(group.end - group.start)}</span>
            <span
              role="button"
              tabIndex={0}
              onClick={(e) => { e.stopPropagation(); setOpen((v) => !v); }}
              onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setOpen((v) => !v); } }}
              className="inline-flex h-5 w-5 items-center justify-center rounded text-ink-4 hover:text-ink-2"
              title={open ? tt('detail.agent.collapse') : tt('detail.agent.expand')}
            >
              <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                   strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"
                   className={open ? 'rotate-90 transition-transform' : 'transition-transform'}>
                <path d="M9 6l6 6-6 6" />
              </svg>
            </span>
          </div>
        </div>
      </button>

      {open && (
        <div className={'border-t border-[color:var(--glass-line)] ' + (hasActiveChild ? 'bg-white/40' : '')}>
          {group.callSpans.length === 0 && group.toolSpans.length === 0 ? (
            <div className="px-3 py-3 text-[11px] text-ink-4">{tt('detail.agent.noCalls')}</div>
          ) : (
            <>
              {group.callSpans.map((s, i) => (
                <CallRow
                  key={s.ID}
                  span={s}
                  index={i + 1}
                  active={s.ID === activeSpanId}
                  onSelect={onSelect}
                  t0={t0}
                  showDivider={i > 0}
                />
              ))}
              {group.toolSpans.map((s, i) => (
                <ToolRow
                  key={s.ID}
                  span={s}
                  active={s.ID === activeSpanId}
                  onSelect={onSelect}
                  t0={t0}
                  showDivider={group.callSpans.length > 0 || i > 0}
                />
              ))}
            </>
          )}
        </div>
      )}
    </section>
  );
}

function CallRow({ span, index, active, onSelect, t0, showDivider }: {
  span: Trace;
  index: number;
  active: boolean;
  onSelect: (id: string) => void;
  t0: number;
  showDivider?: boolean;
}) {
  const offset = Math.max(0, startMs(span) - t0);
  return (
    <button
      type="button"
      onClick={() => onSelect(span.ID)}
      className={
        'w-full text-left px-3 py-2 transition ' +
        (showDivider ? 'border-t border-[color:var(--glass-line)] ' : '') +
        (active ? 'bg-indigo-50/70' : 'hover:bg-white/60')
      }
    >
      <div className="flex items-center gap-2">
        <span className={`dot ${statusDot(span.Status)}`} />
        <span className="mono text-[9.5px] text-ink-4 w-7 shrink-0">#{index}</span>
        <span className="font-semibold text-[12.5px] text-ink truncate flex-1">
          {span.Model || 'llm'}
        </span>
        {span.IsStream && (
          <span className="shrink-0 text-[8px] uppercase tracking-[0.12em] text-indigo-600 font-semibold px-1.5 py-0.5 rounded-full bg-indigo-50/80 border border-indigo-200/60">
            stream
          </span>
        )}
        <span className="shrink-0 mono text-[11px] text-ink-3 tnum">{fmtDur(span.LatencyMs)}</span>
      </div>
      <div className="mt-0.5 pl-[18px] flex items-center gap-2 flex-wrap text-[10px] mono text-ink-4 tnum">
        <span>+{fmtDur(offset)}</span>
        <span className="text-ink-5">·</span>
        <span>↓{fmtTokens(span.InputTokens)} ↑{fmtTokens(span.OutputTokens)}</span>
        {span.CachedInputTokens > 0 && <span>cache {fmtTokens(span.CachedInputTokens)}</span>}
        <span className="text-ink-5">·</span>
        <span>{fmtCostFine(span.CostUSD)}</span>
      </div>
    </button>
  );
}

function ToolRow({ span, active, onSelect, t0, showDivider }: {
  span: Trace;
  active: boolean;
  onSelect: (id: string) => void;
  t0: number;
  showDivider?: boolean;
}) {
  const offset = Math.max(0, startMs(span) - t0);
  return (
    <button
      type="button"
      onClick={() => onSelect(span.ID)}
      className={
        'w-full text-left px-3 py-2 transition ' +
        (showDivider ? 'border-t border-[color:var(--glass-line)] ' : '') +
        (active ? 'bg-cyan-50/70' : 'hover:bg-white/60')
      }
    >
      <div className="flex items-center gap-2">
        <span className={`dot ${statusDot(span.Status)}`} />
        <span className="text-[9px] uppercase tracking-[0.10em] font-bold px-1.5 py-0.5 rounded-full border bg-cyan-50 text-cyan-700 border-cyan-200/70">
          tool
        </span>
        <span className="font-semibold text-[12px] text-ink truncate flex-1">{span.SpanName || 'tool'}</span>
        <span className="mono text-[10.5px] text-ink-4 tnum">+{fmtDur(offset)}</span>
        <span className="mono text-[11px] text-ink-3 tnum">{fmtDur(span.LatencyMs)}</span>
      </div>
    </button>
  );
}

/* ── agent detail panel ──────────────────────────────────────── */

function flattenLeaves(node: SpanNode, out: SpanNode[] = []): SpanNode[] {
  for (const c of node.children) {
    // Nested agents have their own section; keep the parent panel focused on
    // this agent's own model/tool calls instead of duplicating sub-agent work.
    if (isAgentSpan(c)) continue;
    out.push(c);
  }
  return out;
}

export function AgentSpanPanel({ node, onSelect }: { node: SpanNode; onSelect: (id: string) => void }) {
  const tt = useT();
  const leaves = useMemo(() => flattenLeaves(node), [node]);
  const roll = useMemo(() => rollupSpans([node, ...leaves]), [node, leaves]);
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
          <Stat label={tool ? tt('detail.agent.toolCalls') : tt('detail.agent.calls')} value={tool ? '1' : String(roll.calls)} />
          <Stat label={tt('detail.agent.duration')} value={fmtDur(roll.end - roll.start)} mono />
          <Stat label={tt('detail.agent.tokens')} value={fmtTokens(roll.input + roll.output)} mono />
          <Stat label={tt('detail.tile.cost')} value={fmtCostFine(roll.cost)} mono />
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-[12px] text-ink-3">
          <span className="text-ink-4">{fmtTs(node.CreatedAt)}</span>
          {node.AgentID && <><span className="text-ink-5">·</span><span>id <span className="mono text-ink-2">{node.AgentID}</span></span></>}
          {node.AgentPath && <><span className="text-ink-5">·</span><span>path <span className="mono text-ink-2">{node.AgentPath}</span></span></>}
          {node.ParentSpanID && <><span className="text-ink-5">·</span><span>parent <CopyableId value={node.ParentSpanID} className="text-ink-2" /></span></>}
        </div>
        {node.synthetic && (
          <div className="mt-3 rounded-md border border-slate-200 bg-slate-50 px-3 py-2 text-[11.5px] text-slate-600">
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
          {tt('detail.agent.children')} · {leaves.length}
        </h3>
        {leaves.length === 0 ? (
          <div className="text-xs text-ink-4">{tt('detail.agent.noChildren')}</div>
        ) : (
          <div className="flex flex-col gap-1.5">
            {leaves.map((c) => (
              <button
                key={c.ID}
                type="button"
                onClick={() => onSelect(c.ID)}
                className="w-full text-left rounded-lg border border-white/70 bg-white/55 px-3 py-2 hover:bg-white/85 transition"
              >
                <div className="flex items-center gap-2 text-[12px]">
                  <span className={`dot ${statusDot(c.Status)}`} />
                  <span className="text-[9px] uppercase tracking-[0.12em] font-bold text-ink-4">{spanKind(c)}</span>
                  <span className="font-semibold text-ink truncate">{isLLMSpan(c) ? (c.Model || 'llm') : (c.SpanName || 'tool')}</span>
                  <span className="ml-auto mono text-[10.5px] text-ink-4 tnum">{fmtDur(c.LatencyMs)}</span>
                </div>
                <div className="mt-0.5 pl-[18px] flex items-center gap-2 text-[10px] mono text-ink-4 tnum">
                  <span>+{fmtDur(Math.max(0, startMs(c) - startMs(node)))}</span>
                  {isLLMSpan(c) && (
                    <>
                      <span className="text-ink-5">·</span>
                      <span>↓{fmtTokens(c.InputTokens)} ↑{fmtTokens(c.OutputTokens)}</span>
                      <span className="text-ink-5">·</span>
                      <span>{fmtCostFine(c.CostUSD)}</span>
                    </>
                  )}
                </div>
              </button>
            ))}
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
