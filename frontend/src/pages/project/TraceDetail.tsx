import { useQuery } from '@tanstack/react-query';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { api } from '../../lib/api';
import type { ListResp, Trace } from '../../lib/types';
import { fmtCost, fmtCostFine, fmtDur, fmtTokens, fmtTs } from '../../lib/fmt';
import ChatViewer from '../../components/ChatViewer';
import { useT } from '../../i18n';

/* ── types ───────────────────────────────────────────────────── */
interface TimelineEvt { event: string; ts: number; }

/* ── helpers ─────────────────────────────────────────────────── */
const toneCls = (s: string) => {
  if (s === 'error')   return 'bg-rose-50 text-rose-700 border-rose-200/70';
  if (s === 'aborted') return 'bg-amber-50 text-amber-700 border-amber-200/70';
  return 'bg-emerald-50 text-emerald-700 border-emerald-200/70';
};
const dotCls = (s: string) => s === 'error' ? 'err' : s === 'aborted' ? 'warn' : 'ok';

/* ── page ────────────────────────────────────────────────────── */
export default function TraceDetail() {
  const { projectId = '', traceId = '' } = useParams();
  const [search, setSearch] = useSearchParams();
  const selectedSpan = search.get('span');
  const tt = useT();

  const spansQ = useQuery({
    queryKey: ['trace_spans', traceId],
    enabled: !!traceId,
    queryFn: () =>
      api.get<ListResp<Trace>>('/traces?limit=200&trace_id=' + encodeURIComponent(traceId)),
  });

  const items = spansQ.data?.items ?? [];
  const first = items[0];
  const isSingleSpan = items.length === 1;

  // Selection model:
  //   multi-span, no `?span`  → trace summary (root row in tree)
  //   ?span=<id>              → that span's detail
  //   single-span             → always span detail (no separate root view)
  const isRootSelected = !selectedSpan && !isSingleSpan;
  const activeSpanId   = selectedSpan || (isSingleSpan && first ? first.ID : '');

  const selectRoot = () => {
    if (search.has('span')) {
      const next = new URLSearchParams(search);
      next.delete('span');
      setSearch(next, { replace: true });
    }
  };
  const selectSpan = (id: string) => {
    const next = new URLSearchParams(search);
    next.set('span', id);
    setSearch(next, { replace: true });
  };

  // overall trace stats from span aggregate
  const t0 = first ? new Date(first.CreatedAt).getTime() : 0;
  const last = items[items.length - 1];
  const tn = last ? new Date(last.CreatedAt).getTime() + (last.LatencyMs || 0) : 0;
  const totalDur   = tn - t0;
  const totalIn    = items.reduce((s, x) => s + (x.InputTokens || 0), 0);
  const totalOut   = items.reduce((s, x) => s + (x.OutputTokens || 0), 0);
  const totalCost  = items.reduce((s, x) => s + (x.CostUSD || 0), 0);
  const worst = items.reduce(
    (acc, x) => x.Status === 'error' ? 'error'
                 : (x.Status === 'aborted' && acc !== 'error') ? 'aborted' : acc,
    'success',
  );

  return (
    <div className="flex flex-col gap-3 reveal r-1">
      {/* ── breadcrumb only; stats moved into the root row + summary panel ─ */}
      <header className="flex items-center gap-2 text-sm flex-wrap">
        <Link
          to={`/projects/${projectId}/traces`}
          className="inline-flex items-center gap-1 text-ink-3 hover:text-ink transition px-2 py-1 rounded-lg hover:bg-white/65"
        >
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor"
               strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M19 12H5M12 19l-7-7 7-7"/>
          </svg>
          {tt('detail.back')}
        </Link>
        <span className="text-ink-4">/</span>
        <span className="font-semibold text-ink truncate">
          {first?.TraceName || <span className="italic text-ink-4 font-normal">{tt('detail.unnamed')}</span>}
        </span>
        <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full
                          text-[11px] font-semibold border ${toneCls(worst)}`}>
          <span className={`dot ${dotCls(worst)}`} />
          {worst}
        </span>
      </header>

      {/* split layout */}
      <div className="grid grid-cols-1 xl:grid-cols-[400px_1fr] gap-4 items-start">
        {/* span tree */}
        <aside className="glass glass-edge p-3 xl:sticky xl:top-3 max-h-[calc(100vh-160px)] overflow-y-auto">
          <div className="px-2 pt-1 pb-2 flex items-center justify-between">
            <div className="text-[10px] uppercase tracking-[0.18em] text-ink-4 font-bold">{tt('detail.spans')}</div>
            <div className="text-[10px] text-ink-4 tnum">{items.length}</div>
          </div>
          {spansQ.isLoading ? (
            <div className="space-y-2 px-1">
              {[...Array(3)].map((_, i) => <div key={i} className="skel h-16 w-full" />)}
            </div>
          ) : items.length === 0 ? (
            <div className="text-xs text-ink-4 p-4">{tt('detail.spans.empty')}</div>
          ) : (
            <div className="flex flex-col gap-1.5">
              {/* root row → trace summary view. Hidden for single-span traces
                  since the lone span IS the trace and rendering both would
                  duplicate identical info. */}
              {!isSingleSpan && (
                <button
                  type="button"
                  onClick={selectRoot}
                  className={
                    'text-left rounded-xl px-3 py-2 transition relative border ' +
                    (isRootSelected
                      ? 'bg-white border-indigo-300/70 shadow-[0_2px_10px_-2px_rgba(99,102,241,0.30)]'
                      : 'bg-white/60 border-white/70 hover:bg-white/85')
                  }
                >
                  <div className="flex items-center gap-2">
                    <span className="w-1.5 h-1.5 rounded-full bg-gradient-to-br from-indigo-500 to-violet-500" />
                    <div className="font-semibold text-[12.5px] text-ink truncate flex-1">
                      {first?.TraceName || '(unnamed)'}
                    </div>
                    <span className="text-[10px] mono text-ink-4 tnum">{fmtDur(totalDur)}</span>
                  </div>
                  <div className="text-[10px] mono text-ink-4 mt-0.5">
                    root · {items.length} spans · {fmtTokens(totalIn + totalOut)} tok · {fmtCost(totalCost)}
                  </div>
                </button>
              )}

              {items.map((s, i) => {
                const start = new Date(s.CreatedAt).getTime() - t0;
                const dur = s.LatencyMs || 0;
                const startPct = totalDur > 0 ? (start / totalDur) * 100 : 0;
                const durPct = totalDur > 0 ? Math.max(2, (dur / totalDur) * 100) : 100;
                const isActive = s.ID === activeSpanId;
                const dotTone = dotCls(s.Status);
                return (
                  <button
                    key={s.ID}
                    onClick={() => selectSpan(s.ID)}
                    className={
                      'text-left rounded-xl border px-3 py-2 transition relative ' +
                      (isActive
                        ? 'bg-white border-indigo-300/70 shadow-[0_2px_10px_-2px_rgba(99,102,241,0.30)]'
                        : 'bg-white/55 border-white/70 hover:bg-white/85')
                    }
                  >
                    {/* nested indent line */}
                    <div className="absolute left-[14px] top-0 bottom-0 w-px bg-[color:var(--glass-line)]"
                         aria-hidden="true" />
                    <div className="flex items-center gap-2 mb-1">
                      <span className="mono text-[10px] text-ink-4 w-5">#{i + 1}</span>
                      <span className={`dot ${dotTone}`} />
                      <span className="font-semibold text-[12.5px] text-ink truncate flex-1">
                        {s.Provider} · {s.Model}
                      </span>
                      {s.IsStream && (
                        <span className="text-[8.5px] uppercase tracking-[0.14em] text-indigo-600
                                         font-semibold px-1.5 py-0.5 rounded-full bg-indigo-50/80 border border-indigo-200/60">
                          stream
                        </span>
                      )}
                      <span className="text-[11px] mono text-ink-4 tnum">{fmtDur(dur)}</span>
                    </div>
                    {/* waterfall bar */}
                    <div className="relative h-1.5 rounded-full bg-slate-200/50 overflow-hidden">
                      <div
                        className="absolute top-0 h-full rounded-full bg-gradient-to-r from-indigo-500 via-violet-500 to-cyan-400"
                        style={{ left: startPct + '%', width: durPct + '%' }}
                      />
                    </div>
                    <div className="mt-1 flex justify-between text-[10px] mono text-ink-4">
                      <span>+{fmtDur(start)}</span>
                      <span>{fmtTokens(s.TotalTokens)} tok · {fmtCostFine(s.CostUSD)}</span>
                    </div>
                  </button>
                );
              })}
            </div>
          )}
        </aside>

        {/* right panel: trace summary (root) OR span detail */}
        <section className="min-w-0">
          {isRootSelected ? (
            <TraceSummaryPanel
              projectId={projectId}
              traceId={traceId}
              name={first?.TraceName || ''}
              status={worst}
              spanCount={items.length}
              totalDur={totalDur}
              totalIn={totalIn}
              totalOut={totalOut}
              totalCost={totalCost}
              startedAt={first?.CreatedAt}
              userId={first?.UserID}
              sessionId={first?.SessionID}
              spans={items}
            />
          ) : (
            <SpanDetailPanel
              spanId={activeSpanId}
              projectId={projectId}
              showTraceMeta={isSingleSpan}
            />
          )}
        </section>
      </div>
    </div>
  );
}

function Sep() { return <span className="text-ink-5 select-none">·</span>; }

/* ── trace summary panel (when root row selected) ────────────── */

interface TraceSummaryProps {
  projectId: string;
  traceId: string;
  name: string;
  status: string;
  spanCount: number;
  totalDur: number;
  totalIn: number;
  totalOut: number;
  totalCost: number;
  startedAt?: string;
  userId?: string;
  sessionId?: string;
  spans: Trace[];
}

function TraceSummaryPanel(p: TraceSummaryProps) {
  const tt = useT();
  return (
    <div className="flex flex-col gap-3">
      {/* compact header */}
      <section className="glass glass-edge px-5 py-4">
        <div className="flex items-center gap-2 flex-wrap">
          <h2 className="text-[17px] font-bold tracking-tight">
            {p.name || <span className="italic text-ink-4 font-normal">(unnamed)</span>}
          </h2>
          <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full
                            text-[11px] font-semibold border ${toneCls(p.status)}`}>
            <span className={`dot ${dotCls(p.status)}`} />
            {p.status}
          </span>
          <span className="ml-auto text-[11px] mono text-ink-4 select-all">{p.traceId}</span>
        </div>

        {/* big KPI tiles */}
        <div className="mt-4 grid grid-cols-2 md:grid-cols-4 gap-2">
          <Tile label={tt('detail.tile.spans')}    value={String(p.spanCount)} />
          <Tile label={tt('detail.tile.totalDur')} value={fmtDur(p.totalDur)} mono />
          <Tile label={tt('detail.tile.tokens')}   value={fmtTokens(p.totalIn) + ' → ' + fmtTokens(p.totalOut)} mono />
          <Tile label={tt('detail.tile.cost')}     value={fmtCost(p.totalCost)} mono />
        </div>

        {/* meta footer */}
        <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-[12px] text-ink-3">
          <span className="text-ink-4">{fmtTs(p.startedAt)}</span>
          {p.userId && (
            <>
              <Sep />
              <Link
                to={`/projects/${p.projectId}/traces?user_id=${encodeURIComponent(p.userId)}`}
                className="mono text-indigo-600 hover:underline"
                title={tt('detail.filter.byUser')}
              >
                user · {p.userId}
              </Link>
            </>
          )}
          {p.sessionId && (
            <>
              <Sep />
              <Link
                to={`/projects/${p.projectId}/traces?session_id=${encodeURIComponent(p.sessionId)}`}
                className="mono text-indigo-600 hover:underline"
                title={tt('detail.filter.bySession')}
              >
                session · {p.sessionId}
              </Link>
            </>
          )}
        </div>
      </section>

      {/* trace input/output — derived from first request body and last response */}
      {p.spans.length > 0 && (
        <>
          <section className="glass glass-edge p-5">
            <SectionTitle>{tt('detail.input')}</SectionTitle>
            <ChatViewer raw={p.spans[0].RequestBody} mode="request" />
          </section>
          <section className="glass glass-edge p-5">
            <SectionTitle>{tt('detail.output')}</SectionTitle>
            <ChatViewer raw={p.spans[p.spans.length - 1].ResponseBody} mode="response" />
          </section>
        </>
      )}

      <p className="text-[11.5px] text-ink-4 px-1">{tt('detail.spanHint')}</p>
    </div>
  );
}

function Tile({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="relative overflow-hidden rounded-2xl border border-white/70 bg-white/55 px-3.5 py-3">
      <div className="text-[10px] uppercase tracking-[0.16em] text-ink-4 font-semibold">{label}</div>
      <div className={'kpi-num text-[22px] mt-0.5 leading-none ' + (mono ? 'mono' : '')}>{value}</div>
    </div>
  );
}

/* ── span detail panel (was the inner of SpanDetail drawer) ──── */

function SpanDetailPanel({ spanId, projectId, showTraceMeta }: { spanId: string; projectId?: string; showTraceMeta?: boolean }) {
  const tt = useT();
  const q = useQuery({
    queryKey: ['span', spanId],
    queryFn: () => api.get<Trace>('/traces/' + spanId),
    enabled: !!spanId,
  });
  const t = q.data;

  if (q.isLoading) {
    return (
      <div className="glass p-6 space-y-3">
        <div className="skel h-16 w-full" />
        <div className="skel h-72 w-full" />
        <div className="skel h-72 w-full" />
      </div>
    );
  }
  if (!t) return <div className="glass p-6 text-sm text-ink-4">{tt('detail.noData')}</div>;

  return (
    <div className="flex flex-col gap-3">
      {/* ── compact header strip ─────────────────────────────────── */}
      <section className="glass glass-edge px-5 py-4">
        <div className="flex items-center gap-2 flex-wrap">
          <h2 className="text-[17px] font-bold tracking-tight">
            {t.IsStream ? 'Stream' : 'Generate'}
            <span className="text-ink-4 font-normal mx-1.5">·</span>
            <span className="mono text-[14px] text-ink-2">{t.Provider} / {t.Model}</span>
          </h2>
          <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full
                            text-[11px] font-semibold border ${toneCls(t.Status)}`}>
            <span className={`dot ${dotCls(t.Status)}`} />
            {t.Status} · {t.StatusCode}
          </span>
          {t.IsStream && (
            <span className="text-[10px] uppercase tracking-[0.14em] text-indigo-600
                             font-semibold px-1.5 py-0.5 rounded-full bg-indigo-50/80 border border-indigo-200/60">
              stream
            </span>
          )}
          <span className="ml-auto text-[11px] mono text-ink-4 select-all">{t.ID}</span>
        </div>

        {/* trace meta (user/session) is normally surfaced in the trace summary
            panel. For single-span traces we render this panel directly with
            no summary view, so opt-in render it here. */}
        {showTraceMeta && (t.UserID || t.SessionID) && (
          <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-[12px] text-ink-3">
            <span className="text-ink-4">{fmtTs(t.CreatedAt)}</span>
            {t.UserID && projectId && (
              <>
                <Sep />
                <Link
                  to={`/projects/${projectId}/traces?user_id=${encodeURIComponent(t.UserID)}`}
                  className="mono text-indigo-600 hover:underline"
                >
                  user · {t.UserID}
                </Link>
              </>
            )}
            {t.SessionID && projectId && (
              <>
                <Sep />
                <Link
                  to={`/projects/${projectId}/traces?session_id=${encodeURIComponent(t.SessionID)}`}
                  className="mono text-indigo-600 hover:underline"
                >
                  session · {t.SessionID}
                </Link>
              </>
            )}
          </div>
        )}

        {/* inline metrics ribbon — span-level only; user/session are already in the trace header */}
        <div className="mt-3 flex flex-wrap items-center gap-x-5 gap-y-1.5">
          <InlineMetric label={tt('detail.tile.latency')} value={fmtDur(t.LatencyMs || 0)} />
          <InlineMetric label="TTFT"                       value={fmtDur(t.TTFTMs)} />
          <InlineMetric label={tt('detail.tile.tokens')}   value={fmtTokens(t.InputTokens) + ' → ' + fmtTokens(t.OutputTokens) + (t.TokensEstimated ? ' est' : '')} />
          {t.ReasoningTokens > 0 && (
            <InlineMetric label={tt('detail.tile.reasoning')} value={fmtTokens(t.ReasoningTokens)} />
          )}
          {t.CachedInputTokens > 0 && (
            <InlineMetric label={tt('detail.tile.cacheRead')} value={fmtTokens(t.CachedInputTokens)} />
          )}
          {t.CacheCreationInputTokens > 0 && (
            <InlineMetric label={tt('detail.tile.cacheWrite')} value={fmtTokens(t.CacheCreationInputTokens)} />
          )}
          <InlineMetric label={tt('detail.tile.cost')}     value={fmtCostFine(t.CostUSD)} />
        </div>
      </section>

      {/* ── Request + Response (the meat) ────────────────────────── */}
      <section className="glass glass-edge p-5">
        <SectionTitle>{tt('detail.section.request')}</SectionTitle>
        <ChatViewer raw={t.RequestBody} mode="request" />
      </section>

      <section className="glass glass-edge p-5">
        <SectionTitle>{tt(t.IsStream ? 'detail.section.streamResponse' : 'detail.section.response')}</SectionTitle>
        <ChatViewer raw={t.ResponseBody} mode="response" />
      </section>

      {/* ── Secondary details — collapsed by default ─────────────── */}
      <details className="glass glass-edge group">
        <summary className="cursor-pointer list-none px-5 py-3 flex items-center gap-2 select-none
                            hover:bg-white/40 transition rounded-3xl">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor"
               strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"
               className="text-ink-4 transition-transform group-open:rotate-90">
            <path d="M9 6l6 6-6 6"/>
          </svg>
          <span className="font-semibold text-[13px] text-ink-2">{tt('detail.section.more')}</span>
          {t.GenDurationMs != null && (
            <span className="ml-auto text-[11px] mono text-ink-4">
              TTFB {fmtDur(t.TTFBMs)} · gen {fmtDur(t.GenDurationMs)} · tps {t.TPS ? t.TPS.toFixed(1) : '—'}
            </span>
          )}
        </summary>
        <div className="px-5 pb-5 pt-3 flex flex-col gap-4 border-t border-[color:var(--glass-line)]">
          <TimelineBlock raw={t.Timeline} title={tt('detail.section.timeline')} />
          <div>
            <SectionTitle>{tt('detail.section.upstreamURL')}</SectionTitle>
            <div className="code-line break-all">{t.RequestPath || '—'}</div>
          </div>
        </div>
      </details>
    </div>
  );
}

function InlineMetric({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-baseline gap-1.5">
      <span className="text-[10px] uppercase tracking-[0.14em] text-ink-4 font-semibold">{label}</span>
      <span className={(mono ? 'mono ' : '') + 'tnum text-[12.5px] text-ink font-medium'}>{value}</span>
    </div>
  );
}

function SectionTitle({ children }: { children: React.ReactNode }) {
  return <h3 className="text-[11px] uppercase tracking-[0.16em] text-ink-4 font-bold mb-3">{children}</h3>;
}

function TimelineBlock({ raw, title }: { raw: string; title?: string }) {
  const tt = useT();
  const eventLabel = (ev: string) => {
    const key = ('detail.timelineEvent.' + ev) as Parameters<typeof tt>[0];
    const v = tt(key);
    return v === key ? ev : v;
  };
  let evts: TimelineEvt[] = [];
  try { evts = JSON.parse(raw || '[]') as TimelineEvt[]; } catch { /* */ }
  if (!Array.isArray(evts) || evts.length === 0) {
    return (
      <>
        <SectionTitle>{title || tt('detail.section.timeline')}</SectionTitle>
        <div className="text-xs text-ink-4">—</div>
      </>
    );
  }
  const t0 = evts[0].ts;
  const total = evts[evts.length - 1].ts - t0;
  return (
    <>
      <h3 className="text-[11px] uppercase tracking-[0.16em] text-ink-4 font-bold mb-3 flex items-baseline gap-2">
        <span>{title || tt('detail.section.timeline')}</span>
        <span className="text-ink-4 tracking-normal normal-case text-[11px] font-normal">{fmtDur(total)}</span>
      </h3>
      <div className="rounded-2xl bg-white/55 border border-white/70 p-3.5">
        <div className="space-y-2">
          {evts.map((ev, i) => {
            const cum = ev.ts - t0;
            const delta = i === 0 ? 0 : ev.ts - evts[i - 1].ts;
            const pct = total > 0 && delta > 0 ? Math.min(100, (delta / total) * 100) : 0;
            return (
              <div key={i} className="flex items-center gap-2 text-[12px]">
                <div className="w-44 text-ink-2 truncate" title={ev.event}>{eventLabel(ev.event)}</div>
                <div className="w-16 text-right tnum mono text-ink-4">+{fmtDur(delta)}</div>
                <div className="flex-1 h-2 rounded-full bg-slate-200/60 overflow-hidden">
                  <div className="h-2 rounded-full bg-gradient-to-r from-indigo-500 via-violet-500 to-cyan-400"
                       style={{ width: pct + '%' }} />
                </div>
                <div className="w-16 text-right tnum mono text-ink-4">{fmtDur(cum)}</div>
              </div>
            );
          })}
        </div>
      </div>
    </>
  );
}

