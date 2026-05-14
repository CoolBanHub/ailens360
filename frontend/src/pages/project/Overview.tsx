import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { api } from '../../lib/api';
import type { ListResp, TraceGroup, UsageItem } from '../../lib/types';
import { fmtCost, fmtDur, fmtNum, fmtTokens, fmtTs } from '../../lib/fmt';
import { useT } from '../../i18n';

interface UsageResp { dimension: string; items: UsageItem[] }

// All KPI tiles share the same neutral card. The previous per-tile color
// tinting added color noise that read as decoration, not data — the
// numbers themselves are what should pop. Keep accent color for the single
// "active" indicator (live-dot, primary CTA), not metric tiles.
const TILE_CLS = 'rounded-2xl border border-white/70 bg-white/65 px-4 py-3.5';

export default function ProjectOverview() {
  const { projectId = '' } = useParams();
  const t = useT();

  const usage = useQuery({
    queryKey: ['proj-usage', projectId],
    queryFn: () =>
      api.get<UsageResp>('/metrics/usage?dimension=model&project_id=' + projectId),
    refetchInterval: 10_000,
    enabled: !!projectId,
  });

  const recent = useQuery({
    queryKey: ['proj-recent', projectId],
    queryFn: () =>
      api.get<ListResp<TraceGroup>>('/trace_groups?limit=5&project_id=' + projectId),
    refetchInterval: 5_000,
    enabled: !!projectId,
  });

  const items = usage.data?.items ?? [];
  const totals = items.reduce(
    (a, it) => ({
      calls:       a.calls       + (it.Calls || 0),
      input:       a.input       + (it.InputTokens || 0),
      output:      a.output      + (it.OutputTokens || 0),
      cached:      a.cached      + (it.CachedInputTokens || 0),
      cacheCreate: a.cacheCreate + (it.CacheCreationInputTokens || 0),
      cost:        a.cost        + (it.CostUSD || 0),
      avgLat:      a.avgLat      + (it.AvgLatencyMs || 0),
      errN:        a.errN        + ((it.ErrorRate || 0) * (it.Calls || 0)),
    }),
    { calls: 0, input: 0, output: 0, cached: 0, cacheCreate: 0, cost: 0, avgLat: 0, errN: 0 },
  );
  const avgLatency = items.length ? Math.round(totals.avgLat / items.length) : 0;
  const errRate = totals.calls ? (totals.errN / totals.calls) * 100 : 0;

  return (
    <div className="flex flex-col gap-5">
      {/* KPI tiles */}
      <div className="grid grid-cols-2 md:grid-cols-3 xl:grid-cols-5 gap-3">
        <BigKpi label={t('overview.kpi.calls')}        value={fmtNum(totals.calls)} />
        <BigKpi label={t('overview.kpi.inTokens')}     value={fmtTokens(totals.input)} />
        <BigKpi label={t('overview.kpi.outTokens')}    value={fmtTokens(totals.output)} />
        <BigKpi label={t('overview.kpi.cacheTokens')}  value={fmtTokens(totals.cached + totals.cacheCreate)} />
        <BigKpi label={t('overview.kpi.cost')}         value={fmtCost(totals.cost)} />
      </div>

      {/* second-row mini stats + recent traces side by side */}
      <div className="grid lg:grid-cols-[1fr_1.2fr] gap-4">
        <section className="glass glass-edge p-5">
          <h3 className="text-[11px] uppercase tracking-[0.16em] text-ink-4 font-semibold mb-3">{t('overview.health')}</h3>
          <div className="grid grid-cols-2 gap-3">
            <MiniStat label={t('overview.health.latency')} value={fmtDur(avgLatency)} accent="ok" />
            <MiniStat label={t('overview.health.errRate')} value={errRate.toFixed(2) + '%'} accent={errRate > 1 ? 'warn' : 'ok'} />
            <MiniStat label={t('overview.health.models')}  value={String(items.length)} />
            <MiniStat label={t('overview.health.active')}  value={fmtNum(recent.data?.total ?? 0)} />
          </div>

          <h3 className="text-[11px] uppercase tracking-[0.16em] text-ink-4 font-semibold mt-6 mb-3">
            {t('overview.byModel')}
          </h3>
          {usage.isLoading ? (
            <div className="space-y-2">
              {[...Array(3)].map((_, i) => <div key={i} className="skel h-7 w-full" />)}
            </div>
          ) : items.length === 0 ? (
            <p className="text-xs text-ink-4">{t('overview.byModel.empty')}</p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="text-[10px] uppercase tracking-[0.14em] text-ink-4 text-left">
                  <th className="pb-2 font-semibold">{t('overview.byModel.col.model')}</th>
                  <th className="pb-2 font-semibold text-right">{t('overview.byModel.col.calls')}</th>
                  <th className="pb-2 font-semibold text-right">{t('overview.byModel.col.cost')}</th>
                </tr>
              </thead>
              <tbody>
                {items.map((it) => (
                  <tr key={it.Key} className="border-t border-[color:var(--glass-line)]">
                    <td className="py-2 mono text-[12px] truncate max-w-[200px]">{it.Key}</td>
                    <td className="py-2 text-right tnum">{fmtNum(it.Calls)}</td>
                    <td className="py-2 text-right tnum font-semibold">{fmtCost(it.CostUSD)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>

        {/* Recent traces */}
        <section className="glass glass-edge p-5">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-[11px] uppercase tracking-[0.16em] text-ink-4 font-semibold">{t('overview.recent')}</h3>
            <Link to={`/projects/${projectId}/traces`}
                  className="text-xs font-semibold text-indigo-600 hover:underline">
              {t('overview.viewAll')} →
            </Link>
          </div>
          {recent.isLoading ? (
            <div className="space-y-2">
              {[...Array(4)].map((_, i) => <div key={i} className="skel h-12 w-full" />)}
            </div>
          ) : (recent.data?.items?.length ?? 0) === 0 ? (
            <p className="text-xs text-ink-4">{t('overview.recent.empty')}</p>
          ) : (
            <div className="flex flex-col gap-1.5">
              {(recent.data?.items ?? []).map((g) => (
                <Link
                  key={g.TraceID}
                  to={`/projects/${projectId}/traces?trace=${encodeURIComponent(g.TraceID)}`}
                  className="flex items-center gap-3 px-3 py-2.5 rounded-2xl
                             bg-white/55 hover:bg-white/85 border border-white/70 transition"
                >
                  <span className={`dot ${g.Status === 'error' ? 'err' : g.Status === 'aborted' ? 'warn' : 'ok'}`} />
                  <div className="flex-1 min-w-0">
                    <div className="text-sm font-semibold truncate">
                      {g.TraceName || <span className="italic text-ink-4 font-normal">(unnamed)</span>}
                    </div>
                    <div className="text-[11px] mono text-ink-4 truncate">{g.TraceID}</div>
                  </div>
                  <div className="text-right shrink-0">
                    <div className="text-[12px] tnum">{g.SpanCount} spans · {fmtDur(g.LatencyMs)}</div>
                    <div className="text-[11px] text-ink-4">{fmtTs(g.StartedAt)}</div>
                  </div>
                </Link>
              ))}
            </div>
          )}
        </section>
      </div>
    </div>
  );
}

function BigKpi({ label, value }: { label: string; value: string }) {
  return (
    <div className={TILE_CLS}>
      <div className="text-[10px] uppercase tracking-[0.16em] text-ink-4 font-semibold">{label}</div>
      <div className="kpi-num text-[26px] mt-1 leading-none">{value}</div>
    </div>
  );
}

function MiniStat({ label, value, accent = 'ok' }: { label: string; value: string; accent?: 'ok'|'warn'|'err' }) {
  return (
    <div className="glass-faint relative px-3.5 py-3">
      <div className="text-[10px] uppercase tracking-[0.14em] text-ink-4 font-semibold flex items-center gap-1.5">
        <span className={`dot ${accent}`} />
        {label}
      </div>
      <div className="kpi-num text-[17px] mt-1">{value}</div>
    </div>
  );
}
