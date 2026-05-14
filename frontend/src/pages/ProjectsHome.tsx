import { FormEvent, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import type { ListResp, Project } from '../lib/types';
import { fmtTsSec } from '../lib/fmt';
import { useT } from '../i18n';

function IcPlus() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
         strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
      <path d="M12 5v14M5 12h14"/>
    </svg>
  );
}
function IcGear() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
         strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="12" cy="12" r="3"/>
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33h.01a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51h.01a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z"/>
    </svg>
  );
}
function IcArrow() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
         strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
      <path d="M5 12h14M13 5l7 7-7 7"/>
    </svg>
  );
}

export default function ProjectsHome() {
  const qc = useQueryClient();
  const nav = useNavigate();
  const t = useT();
  const { data, isLoading } = useQuery({
    queryKey: ['projects'],
    queryFn: () => api.get<ListResp<Project>>('/projects'),
  });
  const items = data?.items ?? [];

  // create modal state
  const [createOpen, setCreateOpen] = useState(false);
  const [name, setName] = useState('');
  const create = useMutation({
    mutationFn: (n: string) => api.post<Project>('/projects', { name: n }),
    onSuccess: (p) => {
      setName('');
      setCreateOpen(false);
      qc.invalidateQueries({ queryKey: ['projects'] });
      // jump straight into the new project
      nav(`/projects/${p.id}/setup`);
    },
  });

  function submit(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    create.mutate(name.trim());
  }

  return (
    <div className="flex flex-col gap-7">
      {/* Header strip — minimalist title row, big New button */}
      <div className="flex items-end justify-between gap-3 reveal r-1">
        <div>
          <h1 className="text-[26px] font-bold tracking-tight leading-tight">{t('projects.title')}</h1>
        </div>
        <button onClick={() => setCreateOpen(true)} className="btn-grad">
          <IcPlus />
          {t('projects.new')}
        </button>
      </div>

      {/* Grid */}
      {isLoading ? (
        <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {[...Array(3)].map((_, i) => <div key={i} className="skel h-44 w-full rounded-3xl" />)}
        </div>
      ) : items.length === 0 ? (
        <EmptyState onCreate={() => setCreateOpen(true)} />
      ) : (
        <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {items.map((p, i) => (
            <ProjectCard key={p.id} project={p} delay={i} />
          ))}
        </div>
      )}

      {/* Create dialog */}
      {createOpen && (
        <div className="fixed inset-0 z-50 grid place-items-center px-4">
          <div className="absolute inset-0 bg-slate-900/15 backdrop-blur-md"
               onClick={() => setCreateOpen(false)} />
          <div className="relative glass-strong glass-edge max-w-[460px] w-full p-6 reveal r-1">
            <h3 className="text-lg font-bold tracking-tight mb-5">{t('projects.create.title')}</h3>
            <form onSubmit={submit} className="flex flex-col gap-3">
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="chatbot-prod"
                autoFocus
                className="bg-white/85 border border-white/85 rounded-2xl px-4 py-3 text-sm
                           focus:bg-white outline-none transition"
                required
              />
              {create.isError && (
                <p className="text-xs text-rose-600">{(create.error as Error).message}</p>
              )}
              <div className="flex justify-end gap-2 mt-2">
                <button type="button" className="btn-ghost" onClick={() => setCreateOpen(false)}>
                  {t('projects.create.cancel')}
                </button>
                <button type="submit" className="btn-grad" disabled={create.isPending}>
                  {create.isPending ? t('projects.create.busy') : t('projects.create.submit')}
                  <IcArrow />
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}

/* ── Project card ──────────────────────────────────────────────── */

// note: ProjectCard preview no longer renders the prefix (kept tidy);
// users see the prefix on the project's Setup tab instead.
function ProjectCard({ project, delay }: { project: Project; delay: number }) {
  const t = useT();
  const delayClass = ['r-1', 'r-2', 'r-3', 'r-4', 'r-5', 'r-6'][Math.min(delay, 5)];
  return (
    <Link
      to={`/projects/${project.id}/overview`}
      className={
        'group relative glass glass-edge p-5 reveal ' + delayClass +
        ' hover:translate-y-[-2px] hover:shadow-[0_2px_6px_rgba(15,23,42,0.06),0_24px_48px_-12px_rgba(67,56,202,0.20)] transition'
      }
    >
      {/* decorative corner glow */}
      <div className="absolute -right-8 -top-8 w-32 h-32 rounded-full opacity-0 group-hover:opacity-100
                      transition-opacity duration-500
                      bg-gradient-to-br from-violet-300/40 to-cyan-300/30 blur-2xl pointer-events-none" />

      <div className="relative flex items-start gap-3 mb-5">
        <div className="w-10 h-10 rounded-2xl grid place-items-center text-white font-bold
                        bg-gradient-to-br from-indigo-400 via-violet-400 to-cyan-400
                        shadow-[0_4px_12px_-2px_rgba(99,102,241,0.4)]">
          {project.name[0]?.toUpperCase() || 'P'}
        </div>
        <div className="min-w-0 flex-1">
          <h3 className="font-bold text-base tracking-tight truncate">{project.name}</h3>
          <div className="text-[11px] mono text-ink-4 mt-0.5 truncate">{project.id}</div>
        </div>
      </div>

      <div className="text-[11px] text-ink-4 mb-4 flex items-center gap-1.5">
        <span className="inline-block w-1 h-1 rounded-full bg-ink-4" />
        {t('projects.card.created')} {fmtTsSec(project.created_at)}
      </div>

      <div className="flex items-center gap-2">
        <span className="btn-ghost flex-1 justify-center group-hover:bg-white">
          {t('projects.card.enter')}
          <span className="opacity-0 group-hover:opacity-100 transition-opacity">→</span>
        </span>
        <span
          className="w-9 h-9 grid place-items-center rounded-full bg-white/70 border border-white/80
                     hover:bg-white text-ink-3 hover:text-ink transition"
          onClick={(e) => { e.preventDefault(); e.stopPropagation(); window.location.assign(`/projects/${project.id}/settings`); }}
          title={t('projects.card.settingsTooltip')}
        >
          <IcGear />
        </span>
      </div>
    </Link>
  );
}

function EmptyState({ onCreate }: { onCreate: () => void }) {
  const t = useT();
  return (
    <div className="glass p-12 text-center reveal r-2">
      <div className="mx-auto mb-4 w-16 h-16 rounded-3xl grid place-items-center
                      bg-gradient-to-br from-indigo-100 to-violet-100 border border-white/70">
        <svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor"
             strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="text-indigo-500">
          <path d="M3 7h7l2 2h9v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z"/>
        </svg>
      </div>
      <h3 className="text-lg font-bold">{t('projects.empty.title')}</h3>
      <p className="text-sm text-ink-3 mt-1 max-w-[420px] mx-auto">{t('projects.empty.body')}</p>
      <button onClick={onCreate} className="btn-grad mt-5 inline-flex">
        <IcPlus />
        {t('projects.empty.cta')}
      </button>
    </div>
  );
}
