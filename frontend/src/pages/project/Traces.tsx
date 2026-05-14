import { useQuery } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { api } from '../../lib/api';
import type { ListResp, TraceGroup } from '../../lib/types';
import { fmtCost, fmtDur, fmtTs } from '../../lib/fmt';
import { TokenCell } from '../../components/TokenCell';
import { CopyableId } from '../../components/CopyableId';
import { useT } from '../../i18n';

const statusTone = (s: string) =>
  s === 'error' ? 'err' : s === 'aborted' ? 'warn' : 'ok';

type StatusKey = '' | 'success' | 'error' | 'aborted';
type TimeKey   = '' | '1h' | '24h' | '7d' | '30d' | 'custom';

const TIME_PRESETS: Record<Exclude<TimeKey, '' | 'custom'>, number> = {
  '1h':  60 * 60 * 1000,
  '24h': 24 * 60 * 60 * 1000,
  '7d':  7  * 24 * 60 * 60 * 1000,
  '30d': 30 * 24 * 60 * 60 * 1000,
};

interface Facets { models: string[]; has_data: boolean; }

// "2026-05-13T10:00" (datetime-local) → unix ms, in user's local TZ.
function localToMs(s: string): number | null {
  if (!s) return null;
  const t = new Date(s).getTime();
  return Number.isFinite(t) ? t : null;
}

// Build a "now-rounded-to-minute" datetime-local default string in local TZ.
function defaultLocalDateTime(offsetMs = 0): string {
  const d = new Date(Date.now() + offsetMs);
  d.setSeconds(0, 0);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export default function ProjectTraces() {
  const { projectId = '' } = useParams();
  const nav = useNavigate();
  const t = useT();
  const [search] = useSearchParams();

  // ── filter state ─────────────────────────────────────────────
  const [userId,     setUserId]     = useState(search.get('user_id')    || '');
  const [sessionId,  setSessionId]  = useState(search.get('session_id') || '');
  const [traceName,  setTraceName]  = useState(search.get('trace_name') || '');
  const [model,      setModel]      = useState(search.get('model')      || '');
  const [status,     setStatus]     = useState<StatusKey>((search.get('status') as StatusKey) || '');
  const [timeRange,  setTimeRange]  = useState<TimeKey>((search.get('time') as TimeKey) || '24h');
  // Custom datetime-local strings (e.g. "2026-05-13T10:00"). Only used when timeRange === 'custom'.
  const [customStart, setCustomStart] = useState(() => defaultLocalDateTime(-24 * 60 * 60 * 1000));
  const [customEnd,   setCustomEnd]   = useState(() => defaultLocalDateTime());
  // Auto-refresh is opt-in. Polls every 10s when enabled, off otherwise.
  const [autoRefresh, setAutoRefresh] = useState(false);

  // Facets: distinct models for this project, drives the model dropdown.
  const facetsQ = useQuery({
    queryKey: ['proj-trace_facets', projectId],
    enabled: !!projectId,
    queryFn: () => api.get<Facets>('/trace_facets?project_id=' + encodeURIComponent(projectId)),
    staleTime: 60_000,
  });
  const facets = facetsQ.data ?? { models: [], has_data: false };

  const groups = useQuery({
    // queryKey holds only stable filter inputs. "Now" is computed inside
    // queryFn at fetch time — otherwise Date.now() on every render would
    // change the key and trigger a fetch storm.
    queryKey: ['proj-trace_groups', projectId, userId, sessionId, traceName, model, status, timeRange, customStart, customEnd],
    queryFn: () => {
      const startMs = timeRange === 'custom' ? localToMs(customStart) :
                      timeRange !== '' ? Date.now() - TIME_PRESETS[timeRange] : null;
      const endMs   = timeRange === 'custom' ? localToMs(customEnd) : null;
      const q = new URLSearchParams({ limit: '50', project_id: projectId });
      if (userId)    q.set('user_id', userId);
      if (sessionId) q.set('session_id', sessionId);
      if (traceName) q.set('trace_name', traceName);
      if (model)     q.set('model', model);
      if (status)    q.set('status', status);
      if (startMs != null) q.set('start_time', String(startMs));
      if (endMs   != null) q.set('end_time',   String(endMs));
      return api.get<ListResp<TraceGroup>>('/trace_groups?' + q.toString());
    },
    refetchInterval: autoRefresh ? 10_000 : false,
    enabled: !!projectId,
  });

  const items = groups.data?.items ?? [];
  const openTrace = (traceId: string) =>
    nav(`/projects/${projectId}/traces/${encodeURIComponent(traceId)}`);

  // Non-default time = anything other than "all"; treat it as an active filter
  // for the purpose of the empty-state message ("no matches in this window")
  // versus the project's onboarding empty-state ("project has no traces yet").
  const hasActiveFilter = !!(userId || sessionId || traceName || model || status || timeRange !== '');
  function clearAll() {
    setUserId(''); setSessionId(''); setTraceName('');
    setModel(''); setStatus('');
    setTimeRange('');
  }
  // Authoritative existence flag from the facets endpoint — covers traces
  // with empty/unknown model that would otherwise miss the model-facet
  // signal. Used to pick between onboarding empty vs. no-match empty.
  const projectHasData = facets.has_data;

  return (
    <div className="flex flex-col gap-5">
      {/* Filter bar — relative z-20 so its child dropdowns stack above the
          trace-list section (both create stacking contexts via .glass's
          backdrop-filter, and DOM order would otherwise win). */}
      <section className="relative z-20 glass glass-edge p-3">
        <div className="flex flex-wrap items-center gap-2">
          <TimeRangePicker
            timeRange={timeRange}
            customStart={customStart}
            customEnd={customEnd}
            onChange={(tr, s, e) => {
              setTimeRange(tr);
              if (s !== undefined) setCustomStart(s);
              if (e !== undefined) setCustomEnd(e);
            }}
          />
          <FilterMenu
            label={t('traces.filter.status')}
            value={status}
            options={[
              ['',         t('traces.filter.all')],
              ['success',  t('traces.status.success')],
              ['error',    t('traces.status.error')],
              ['aborted',  t('traces.status.aborted')],
            ]}
            onChange={(v) => setStatus(v as StatusKey)}
          />
          <FilterMenu
            label={t('traces.filter.model')}
            value={model}
            options={[
              ['', t('traces.filter.all')],
              ...facets.models.map<[string, string]>((m) => [m, m]),
            ]}
            onChange={setModel}
          />
          <FilterText placeholder={t('traces.filter.name')}    value={traceName} onChange={setTraceName} width={160} />
          <FilterText placeholder={t('traces.filter.user')}    value={userId}    onChange={setUserId}    width={140} />
          <FilterText placeholder={t('traces.filter.session')} value={sessionId} onChange={setSessionId} width={170} />

          <button
            onClick={() => groups.refetch()}
            className="btn-ghost"
            disabled={groups.isFetching}
          >
            {groups.isFetching ? t('traces.filter.refreshing') : t('traces.filter.refresh')}
          </button>
          <AutoRefreshToggle on={autoRefresh} onChange={setAutoRefresh} label={t('traces.filter.autoRefresh')} />
          {hasActiveFilter && (
            <button
              onClick={clearAll}
              className="text-xs text-ink-4 hover:text-ink-2 underline-offset-2 hover:underline px-2"
            >
              {t('traces.filter.clear')}
            </button>
          )}
          <span className="ml-auto text-xs text-ink-4">
            {t('traces.total', { n: groups.data?.total ?? 0 })}
          </span>
        </div>
      </section>

      {/* Trace list */}
      <section className="relative z-10 glass glass-edge overflow-hidden">
        {groups.isLoading ? (
          <div className="p-4 space-y-2.5">
            {[...Array(6)].map((_, i) => <div key={i} className="skel h-14 w-full" />)}
          </div>
        ) : items.length === 0 ? (
          projectHasData
            ? <NoMatchTraces hasActiveFilter={hasActiveFilter} onReset={clearAll} />
            : <EmptyTraces />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-[11px] uppercase tracking-[0.14em] text-ink-4 text-left
                               border-b border-[color:var(--glass-line)]">
                  <th className="py-3.5 pl-6 pr-3 font-semibold">{t('traces.col.time')}</th>
                  <th className="py-3.5 px-3 font-semibold">{t('traces.col.trace')}</th>
                  <th className="py-3.5 px-3 font-semibold">{t('traces.col.user')}</th>
                  <th className="py-3.5 px-3 font-semibold">{t('traces.col.session')}</th>
                  <th className="py-3.5 px-3 font-semibold text-right"><span className="inline-block -mr-[0.14em]">{t('traces.col.spans')}</span></th>
                  <th className="py-3.5 px-3 font-semibold">{t('traces.col.status')}</th>
                  <th className="py-3.5 px-3 font-semibold">{t('traces.col.latency')}</th>
                  <th className="py-3.5 px-3 font-semibold">{t('traces.col.tokens')}</th>
                  <th className="py-3.5 px-3 pr-6 font-semibold text-right"><span className="inline-block -mr-[0.14em]">{t('traces.col.cost')}</span></th>
                </tr>
              </thead>
              <tbody>
                {items.map((g, i) => {
                  const tone = statusTone(g.Status);
                  const toneCls =
                    tone === 'err'  ? 'bg-rose-50 text-rose-700 border-rose-200/70'  :
                    tone === 'warn' ? 'bg-amber-50 text-amber-700 border-amber-200/70':
                                      'bg-emerald-50 text-emerald-700 border-emerald-200/70';
                  const statusLabel =
                    g.Status === 'error'   ? t('traces.status.error') :
                    g.Status === 'aborted' ? t('traces.status.aborted') :
                                             t('traces.status.success');
                  return (
                    <tr
                      key={g.TraceID}
                      onClick={() => openTrace(g.TraceID)}
                      className={
                        'cursor-pointer border-b border-[color:var(--glass-line)] last:border-0 ' +
                        'transition-colors hover:bg-white/55 ' +
                        (i % 2 === 0 ? 'bg-white/0' : 'bg-white/[0.03]')
                      }
                    >
                      <td className="py-3.5 pl-6 pr-3 mono text-[12.5px] text-ink-3 whitespace-nowrap">
                        {fmtTs(g.StartedAt)}
                      </td>
                      <td className="py-3.5 px-3 min-w-[240px]">
                        <div className="font-semibold text-ink leading-tight">
                          {g.TraceName || <span className="text-ink-4 font-normal italic">(unnamed)</span>}
                        </div>
                        <CopyableId value={g.TraceID} className="text-[11px] text-ink-4 mt-0.5" />
                      </td>
                      <td className="py-3.5 px-3">
                        {g.UserID
                          ? <div className="max-w-[160px]"><CopyableId value={g.UserID} className="text-[12px] text-ink-2 w-full" /></div>
                          : <span className="mono text-[12px] text-ink-4">—</span>}
                      </td>
                      <td className="py-3.5 px-3">
                        {g.SessionID
                          ? <div className="max-w-[200px]"><CopyableId value={g.SessionID} className="text-[12px] text-ink-2 w-full" /></div>
                          : <span className="mono text-[12px] text-ink-4">—</span>}
                      </td>
                      <td className="py-3.5 px-3 text-right tnum">
                        <span className="inline-flex items-center justify-center min-w-[26px]
                                         px-2 py-0.5 rounded-full bg-white/70 border border-white/80
                                         text-[12px] font-semibold">
                          {g.SpanCount}
                        </span>
                      </td>
                      <td className="py-3.5 px-3">
                        <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full
                                          text-[11px] font-semibold border ${toneCls}`}>
                          <span className={`dot ${tone}`} />
                          {statusLabel}
                        </span>
                      </td>
                      <td className="py-3.5 px-3 tnum text-ink-3">{fmtDur(g.LatencyMs)}</td>
                      <td className="py-3.5 px-3 tnum">
                        <TokenCell
                          size="md"
                          align="left"
                          tokens={{
                            input: g.InputTokens,
                            output: g.OutputTokens,
                            cached: g.CachedInputTokens,
                            cacheCreate: g.CacheCreationInputTokens,
                            reasoning: g.ReasoningTokens,
                            total: g.TotalTokens,
                          }}
                        />
                      </td>
                      <td className="py-3.5 px-3 pr-6 text-right tnum font-semibold">
                        {fmtCost(g.CostUSD)}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}

/* ── filter primitives ───────────────────────────────────────── */

function FilterText({ placeholder, value, onChange, width = 140 }:
  { placeholder: string; value: string; onChange: (v: string) => void; width?: number }) {
  return (
    <input
      placeholder={placeholder}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      style={{ width }}
      className="bg-white/80 border border-white/80 rounded-full px-4 py-1.5
                 text-[12.5px] focus:bg-white outline-none transition"
    />
  );
}

function AutoRefreshToggle({ on, onChange, label }: { on: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      onClick={() => onChange(!on)}
      className={
        'inline-flex items-center gap-2 px-3 py-1.5 rounded-full text-[12.5px] transition border ' +
        (on
          ? 'bg-emerald-50 text-emerald-700 border-emerald-200/70 font-semibold'
          : 'bg-white/80 text-ink-3 border-white/80 hover:bg-white')
      }
    >
      <span
        className={
          'relative inline-flex items-center w-7 h-4 rounded-full transition-colors ' +
          (on ? 'bg-emerald-500' : 'bg-slate-300')
        }
        aria-hidden
      >
        <span
          className={
            'absolute top-0.5 w-3 h-3 rounded-full bg-white shadow-sm transition-transform ' +
            (on ? 'translate-x-3.5' : 'translate-x-0.5')
          }
        />
      </span>
      <span>{label}</span>
      {on && <span className="dot ok" />}
    </button>
  );
}

/* ── time range picker ─────────────────────────────────────────
   A single pill that opens a popover containing both quick presets and a
   custom date-range section. Replaces the old "dropdown + inline-after
   datetime inputs" combo which caused layout jumps and disconnected UX. */

interface TimeRangePickerProps {
  timeRange: TimeKey;
  customStart: string;   // datetime-local
  customEnd:   string;   // datetime-local
  onChange: (tr: TimeKey, customStart?: string, customEnd?: string) => void;
}

const TIME_PRESET_KEYS: Exclude<TimeKey, 'custom'>[] = ['1h', '24h', '7d', '30d', ''];

function formatLocalShort(s: string): string {
  // "2026-05-13T14:30" → "5/13 14:30"
  if (!s || s.length < 16) return s;
  return `${parseInt(s.slice(5, 7), 10)}/${parseInt(s.slice(8, 10), 10)} ${s.slice(11, 16)}`;
}

function TimeRangePicker({ timeRange, customStart, customEnd, onChange }: TimeRangePickerProps) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [draftStart, setDraftStart] = useState(customStart);
  const [draftEnd,   setDraftEnd]   = useState(customEnd);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    setDraftStart(customStart);
    setDraftEnd(customEnd);
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', onDoc);
    return () => document.removeEventListener('mousedown', onDoc);
  }, [open, customStart, customEnd]);

  const presetLabel = (k: Exclude<TimeKey, 'custom'>) =>
    k === '1h'  ? t('traces.filter.time.1h')  :
    k === '24h' ? t('traces.filter.time.24h') :
    k === '7d'  ? t('traces.filter.time.7d')  :
    k === '30d' ? t('traces.filter.time.30d') :
                  t('traces.filter.time.all');

  // Label shown on the pill button.
  const buttonLabel =
    timeRange === 'custom'
      ? `${formatLocalShort(customStart)} → ${formatLocalShort(customEnd)}`
      : presetLabel(timeRange);

  const isDefault = timeRange === '24h';

  function pickPreset(k: Exclude<TimeKey, 'custom'>) {
    onChange(k);
    setOpen(false);
  }

  function applyCustom() {
    onChange('custom', draftStart, draftEnd);
    setOpen(false);
  }

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => setOpen((v) => !v)}
        className={
          'inline-flex items-center gap-2 px-3 py-1.5 rounded-full text-[12.5px] transition border ' +
          (isDefault
            ? 'bg-white/80 text-ink-3 border-white/80 hover:bg-white'
            : 'bg-indigo-50/85 text-indigo-700 border-indigo-200/70 font-semibold')
        }
        aria-haspopup="dialog"
        aria-expanded={open}
      >
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor"
             strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="opacity-80">
          <rect x="3" y="4" width="18" height="18" rx="3"/>
          <path d="M16 2v4M8 2v4M3 10h18"/>
        </svg>
        <span className="text-[10px] uppercase tracking-[0.14em] text-ink-4 font-semibold">
          {t('traces.filter.timeRange')}
        </span>
        <span>{buttonLabel}</span>
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor"
             strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" className="text-ink-4">
          <path d="M6 9l6 6 6-6"/>
        </svg>
      </button>

      {open && (
        <div
          role="dialog"
          className="absolute left-0 mt-1.5 z-50 glass-strong glass-edge p-3 w-[340px]"
        >
          <div className="text-[10px] uppercase tracking-[0.14em] text-ink-4 font-semibold px-1 mb-1.5">
            {t('traces.filter.time.quick')}
          </div>
          <div className="grid grid-cols-3 gap-1.5 mb-3">
            {TIME_PRESET_KEYS.map((k) => {
              const active = timeRange === k;
              return (
                <button
                  key={k || 'all'}
                  onClick={() => pickPreset(k)}
                  className={
                    'px-2.5 py-1.5 rounded-lg text-[12.5px] text-center transition-colors ' +
                    (active
                      ? 'bg-indigo-500/10 ring-1 ring-indigo-300/40 text-indigo-700 font-semibold'
                      : 'bg-white/55 text-ink-2 hover:bg-indigo-50 hover:text-ink')
                  }
                >
                  {presetLabel(k)}
                </button>
              );
            })}
          </div>

          <div className="border-t border-[color:var(--glass-line)] pt-3">
            <div className="text-[10px] uppercase tracking-[0.14em] text-ink-4 font-semibold px-1 mb-2">
              {t('traces.filter.time.custom')}
            </div>
            <label className="flex items-center gap-2 mb-1.5">
              <span className="w-8 text-[11px] text-ink-4">{t('traces.filter.time.start')}</span>
              <input
                type="datetime-local"
                value={draftStart}
                onChange={(e) => setDraftStart(e.target.value)}
                className="flex-1 bg-white/85 border border-white/80 rounded-lg px-2.5 py-1.5
                           text-[12px] mono text-ink-2 focus:bg-white outline-none transition"
              />
            </label>
            <label className="flex items-center gap-2 mb-3">
              <span className="w-8 text-[11px] text-ink-4">{t('traces.filter.time.end')}</span>
              <input
                type="datetime-local"
                value={draftEnd}
                onChange={(e) => setDraftEnd(e.target.value)}
                className="flex-1 bg-white/85 border border-white/80 rounded-lg px-2.5 py-1.5
                           text-[12px] mono text-ink-2 focus:bg-white outline-none transition"
              />
            </label>
            <div className="flex items-center justify-end gap-2">
              <button
                onClick={() => setOpen(false)}
                className="text-[12px] text-ink-4 hover:text-ink-2 px-2 py-1"
              >
                {t('common.cancel')}
              </button>
              <button
                onClick={applyCustom}
                disabled={!draftStart || !draftEnd || (localToMs(draftStart) ?? 0) >= (localToMs(draftEnd) ?? 0)}
                className="btn-grad text-[12px] py-1 px-3 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {t('traces.filter.time.apply')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

interface FilterMenuProps {
  label: string;
  value: string;
  options: [string, string][];  // [value, displayLabel]
  onChange: (v: string) => void;
}

function FilterMenu({ label, value, options, onChange }: FilterMenuProps) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', onDoc);
    return () => document.removeEventListener('mousedown', onDoc);
  }, [open]);

  const current = options.find(([v]) => v === value)?.[1] ?? options[0][1];
  const active = value !== '';

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => setOpen((v) => !v)}
        className={
          'inline-flex items-center gap-2 px-3 py-1.5 rounded-full text-[12.5px] transition ' +
          (active
            ? 'bg-indigo-50/85 text-indigo-700 border border-indigo-200/70 font-semibold'
            : 'bg-white/80 text-ink-3 border border-white/80 hover:bg-white')
        }
      >
        <span className="text-[10px] uppercase tracking-[0.14em] text-ink-4 font-semibold">{label}</span>
        <span>{current}</span>
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor"
             strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" className="text-ink-4">
          <path d="M6 9l6 6 6-6"/>
        </svg>
      </button>
      {open && (
        <div className="absolute left-0 mt-1.5 z-50 glass-strong glass-edge p-1.5 min-w-[220px] w-max max-w-[420px]">
          {options.map(([v, lbl]) => (
            <button
              key={v}
              onClick={() => { onChange(v); setOpen(false); }}
              className={
                'w-full text-left flex items-center gap-2 px-2.5 py-1.5 rounded-lg text-[13px] transition-colors ' +
                (v === value
                  ? 'bg-indigo-500/10 ring-1 ring-indigo-300/40 font-semibold text-indigo-700'
                  : 'text-ink-2 hover:bg-indigo-50 hover:text-ink')
              }
            >
              <span className="flex-1 whitespace-nowrap">{lbl}</span>
              {v === value && <span className="dot ok" />}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function EmptyTraces() {
  const t = useT();
  return (
    <div className="py-16 text-center">
      <div className="mx-auto mb-4 w-14 h-14 rounded-2xl grid place-items-center
                      bg-gradient-to-br from-indigo-100 to-cyan-100 border border-white/70">
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor"
             strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="text-indigo-500">
          <circle cx="12" cy="12" r="10"/>
          <path d="M12 8v4l3 2"/>
        </svg>
      </div>
      <div className="text-base font-bold">{t('traces.empty.title')}</div>
      <p className="text-sm text-ink-3 mt-1 max-w-[420px] mx-auto">{t('traces.empty.body')}</p>
    </div>
  );
}

function NoMatchTraces({ hasActiveFilter, onReset }: { hasActiveFilter: boolean; onReset: () => void }) {
  const t = useT();
  return (
    <div className="py-16 text-center">
      <div className="mx-auto mb-4 w-14 h-14 rounded-2xl grid place-items-center
                      bg-gradient-to-br from-amber-100 to-rose-100 border border-white/70">
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor"
             strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="text-amber-600">
          <circle cx="11" cy="11" r="7"/>
          <path d="m21 21-4.3-4.3"/>
        </svg>
      </div>
      <div className="text-base font-bold">{t('traces.noMatch.title')}</div>
      <p className="text-sm text-ink-3 mt-1 max-w-[420px] mx-auto">{t('traces.noMatch.body')}</p>
      {hasActiveFilter && (
        <button onClick={onReset} className="mt-4 btn-ghost">
          {t('traces.noMatch.reset')}
        </button>
      )}
    </div>
  );
}
