import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { api } from '../../lib/api';
import type { ListResp, Project } from '../../lib/types';
import { SecretField } from '../../components/SecretField';
import { useT } from '../../i18n';

export default function ProjectSettings() {
  const { projectId = '' } = useParams();
  const qc = useQueryClient();
  const nav = useNavigate();
  const t = useT();

  const { data } = useQuery({
    queryKey: ['projects'],
    queryFn: () => api.get<ListResp<Project>>('/projects'),
    staleTime: 30_000,
  });
  const p = data?.items?.find((x) => x.id === projectId);

  const [name, setName] = useState(p?.name || '');
  useEffect(() => { if (p) setName(p.name); }, [p]);

  const rename = useMutation({
    mutationFn: (n: string) => api.put<Project>('/projects/' + projectId, { name: n }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['projects'] }),
  });

  const del = useMutation({
    mutationFn: () => api.del<void>('/projects/' + projectId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['projects'] });
      nav('/projects', { replace: true });
    },
  });

  const resetKey = useMutation({
    mutationFn: () => api.post<Project>('/projects/' + projectId + '/reset_project_key'),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['projects'] }),
  });

  const [confirmDel, setConfirmDel] = useState(false);
  const [confirmReset, setConfirmReset] = useState(false);

  if (!p) return <div className="skel h-40 w-full rounded-3xl" />;

  return (
    <div className="flex flex-col gap-5 max-w-[640px]">
      <section className="glass glass-edge p-6">
        <h2 className="text-base font-bold tracking-tight mb-1">{t('settings.section.basic')}</h2>
        <p className="text-sm text-ink-3 mb-5">
          {t('settings.basic.hint')}
        </p>

        <div className="mb-5">
          <ReadOnly label="project_id" value={p.id} />
        </div>

        <label className="block">
          <span className="text-xs font-semibold text-ink-3 mb-1.5 block">{t('settings.basic.nameLabel')}</span>
          <div className="flex gap-2">
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="flex-1 bg-white/85 border border-white/85 rounded-2xl px-4 py-2.5 text-sm
                         focus:bg-white outline-none transition"
            />
            <button
              onClick={() => rename.mutate(name.trim())}
              disabled={rename.isPending || !name.trim() || name === p.name}
              className="btn-grad"
            >
              {rename.isPending ? t('settings.basic.saving') : t('settings.basic.save')}
            </button>
          </div>
          {rename.isSuccess && <span className="text-xs text-emerald-600 mt-1.5 inline-block">{t('settings.basic.saved')}</span>}
          {rename.isError && <span className="text-xs text-rose-600 mt-1.5 inline-block">{(rename.error as Error).message}</span>}
        </label>
      </section>

      <section className="glass glass-edge p-6">
        <h2 className="text-base font-bold tracking-tight mb-1">{t('settings.section.key')}</h2>
        <p className="text-sm text-ink-3 mb-4">
          {t('settings.key.hint')}
        </p>
        <div className="mb-4">
          <SecretField label="project_key" value={p.project_key} />
        </div>
        {!confirmReset ? (
          <button
            onClick={() => setConfirmReset(true)}
            className="inline-flex items-center gap-2 px-5 py-2.5 rounded-full
                       bg-amber-50 text-amber-800 border border-amber-200 hover:bg-amber-100
                       text-sm font-semibold transition"
          >
            {t('settings.key.reset')}
          </button>
        ) : (
          <div className="flex flex-wrap items-center gap-2">
            <button onClick={() => setConfirmReset(false)} className="btn-ghost">{t('common.cancel')}</button>
            <button
              onClick={() => resetKey.mutate(undefined, { onSettled: () => setConfirmReset(false) })}
              disabled={resetKey.isPending}
              className="inline-flex items-center gap-2 px-5 py-2.5 rounded-full
                         bg-amber-500 hover:bg-amber-600 text-white text-sm font-semibold
                         shadow-[0_8px_22px_-6px_rgba(245,158,11,0.55)]
                         disabled:opacity-60"
            >
              {resetKey.isPending ? t('settings.key.resetting') : t('settings.key.confirmReset')}
            </button>
            <span className="text-xs text-ink-4">{t('settings.key.resetExpire')}</span>
          </div>
        )}
        {resetKey.isSuccess && (
          <div className="text-xs text-emerald-600 mt-3">
            {t('settings.key.resetSuccess')}<code className="mono">{resetKey.data?.project_key}</code>
          </div>
        )}
        {resetKey.isError && (
          <div className="text-xs text-rose-600 mt-3">{(resetKey.error as Error).message}</div>
        )}
      </section>

      <section className="glass glass-edge p-6 border-rose-200/70">
        <h2 className="text-base font-bold text-rose-700 tracking-tight mb-1">{t('settings.section.danger')}</h2>
        <p className="text-sm text-ink-3 mb-4">
          {t('settings.danger.hint')}
        </p>
        {!confirmDel ? (
          <button
            onClick={() => setConfirmDel(true)}
            className="inline-flex items-center gap-2 px-5 py-2.5 rounded-full
                       bg-rose-50 text-rose-700 border border-rose-200 hover:bg-rose-100
                       text-sm font-semibold transition"
          >
            {t('settings.danger.delete', { name: p.name })}
          </button>
        ) : (
          <div className="flex flex-wrap items-center gap-2">
            <button onClick={() => setConfirmDel(false)} className="btn-ghost">{t('common.cancel')}</button>
            <button
              onClick={() => del.mutate()}
              disabled={del.isPending}
              className="inline-flex items-center gap-2 px-5 py-2.5 rounded-full
                         bg-rose-500 hover:bg-rose-600 text-white text-sm font-semibold
                         shadow-[0_8px_22px_-6px_rgba(244,63,94,0.55)]
                         disabled:opacity-60"
            >
              {del.isPending ? t('settings.danger.deleting') : t('settings.danger.confirmDelete')}
            </button>
            <span className="text-xs text-ink-4">{t('settings.danger.irreversible')}</span>
          </div>
        )}
      </section>
    </div>
  );
}

function ReadOnly({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-2xl bg-white/55 border border-white/70 px-3.5 py-2.5">
      <div className="text-[10px] uppercase tracking-[0.14em] text-ink-4 font-semibold">{label}</div>
      <div className="mono text-[12.5px] mt-0.5 break-all">{value}</div>
    </div>
  );
}
