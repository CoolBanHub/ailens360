import { useQuery } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { Link, NavLink, useMatch, useNavigate } from 'react-router-dom';
import { api } from '../lib/api';
import { clearAuth, getAuth } from '../lib/auth';
import type { ListResp, Project } from '../lib/types';
import { useI18n, useT } from '../i18n';
import type { LocaleKey } from '../i18n/locales';

/* ── icons ───────────────────────────────────────────────────── */
const Stroke = { fill: 'none' as const, stroke: 'currentColor',
                 strokeWidth: 1.7, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const };
const Ico = (size = 16) => ({ width: size, height: size, viewBox: '0 0 24 24', ...Stroke });

const IcHome     = () => <svg {...Ico()}><path d="M3 11l9-8 9 8"/><path d="M5 10v10a1 1 0 0 0 1 1h4v-6h4v6h4a1 1 0 0 0 1-1V10"/></svg>;
const IcOverview = () => <svg {...Ico()}><rect x="3" y="3" width="7" height="9" rx="1.5"/><rect x="14" y="3" width="7" height="5" rx="1.5"/><rect x="14" y="12" width="7" height="9" rx="1.5"/><rect x="3" y="16" width="7" height="5" rx="1.5"/></svg>;
const IcTrace    = () => <svg {...Ico()}><path d="M3 6h6M3 12h12M3 18h8"/><circle cx="14" cy="6" r="2"/><circle cx="19" cy="12" r="2"/><circle cx="14" cy="18" r="2"/></svg>;
const IcSession  = () => <svg {...Ico()}><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>;
const IcUser     = () => <svg {...Ico()}><path d="M17 21v-2a4 4 0 0 0-4-4H7a4 4 0 0 0-4 4v2"/><circle cx="10" cy="7" r="4"/><path d="M21 21v-2a4 4 0 0 0-3-3.87"/><path d="M17 3.13a4 4 0 0 1 0 7.75"/></svg>;
const IcSetup    = () => <svg {...Ico()}><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>;
const IcGear     = () => <svg {...Ico()}><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33h.01a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51h.01a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z"/></svg>;
const IcChev     = () => <svg width="11" height="11" viewBox="0 0 24 24" {...Stroke}><path d="M6 9l6 6 6-6"/></svg>;
const IcLogout   = () => <svg {...Ico(14)}><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="M16 17l5-5-5-5"/><path d="M21 12H9"/></svg>;
const IcPlus     = () => <svg {...Ico(13)}><path d="M12 5v14M5 12h14"/></svg>;
const IcCollapse = () => <svg {...Ico(14)}><rect x="3" y="4" width="18" height="16" rx="2"/><line x1="10" y1="4" x2="10" y2="20"/><path d="M16 9l-2 3 2 3"/></svg>;
const IcExpand   = () => <svg {...Ico(14)}><rect x="3" y="4" width="18" height="16" rx="2"/><line x1="10" y1="4" x2="10" y2="20"/><path d="M14 9l2 3-2 3"/></svg>;
const IcGlobe    = () => <svg {...Ico(14)}><circle cx="12" cy="12" r="10"/><path d="M2 12h20"/><path d="M12 2a15 15 0 0 1 0 20a15 15 0 0 1 0-20Z"/></svg>;

/* ── sidebar config ──────────────────────────────────────────── */

interface NavLeaf { to: string; labelKey: LocaleKey; Icon: () => JSX.Element; soon?: boolean; }
interface NavSection { labelKey?: LocaleKey; items: NavLeaf[]; }

const projectSections: NavSection[] = [
  { items: [{ to: 'overview', labelKey: 'nav.module.overview', Icon: IcOverview }] },
  {
    labelKey: 'nav.section.observability',
    items: [
      { to: 'traces',   labelKey: 'nav.module.traces',   Icon: IcTrace },
      { to: 'sessions', labelKey: 'nav.module.sessions', Icon: IcSession, soon: true },
      { to: 'users',    labelKey: 'nav.module.users',    Icon: IcUser,    soon: true },
    ],
  },
  {
    labelKey: 'nav.section.config',
    items: [
      { to: 'setup',    labelKey: 'nav.module.setup',    Icon: IcSetup },
      { to: 'settings', labelKey: 'nav.module.settings', Icon: IcGear },
    ],
  },
];

/* ── collapse state ──────────────────────────────────────────── */

const COLLAPSE_KEY = 'ailens_sidebar_collapsed';
function useCollapsed() {
  const [collapsed, setCollapsed] = useState<boolean>(() => {
    try { return localStorage.getItem(COLLAPSE_KEY) === '1'; } catch { return false; }
  });
  useEffect(() => {
    try { localStorage.setItem(COLLAPSE_KEY, collapsed ? '1' : '0'); } catch { /* */ }
  }, [collapsed]);
  return [collapsed, setCollapsed] as const;
}

/* ── shell ───────────────────────────────────────────────────── */

export default function AppShell({ children }: { children: React.ReactNode }) {
  const projMatch = useMatch('/projects/:projectId/*');
  const projectId = projMatch?.params.projectId || '';
  const inProject = !!projectId;
  const [collapsed, setCollapsed] = useCollapsed();

  return (
    <div className="flex min-h-screen">
      <Sidebar
        projectId={projectId}
        inProject={inProject}
        collapsed={collapsed}
        onToggle={() => setCollapsed((v) => !v)}
      />
      <main className="flex-1 min-w-0">
        <div className="px-6 py-6 lg:px-8 lg:py-7">{children}</div>
      </main>
    </div>
  );
}

/* ── sidebar ─────────────────────────────────────────────────── */

interface SidebarProps {
  projectId: string;
  inProject: boolean;
  collapsed: boolean;
  onToggle: () => void;
}

function Sidebar({ projectId, inProject, collapsed, onToggle }: SidebarProps) {
  const nav = useNavigate();
  const { username } = getAuth();
  const t = useT();
  const { locale, setLocale } = useI18n();
  const [langOpen, setLangOpen] = useState(false);
  const langRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!langOpen) return;
    const onDoc = (e: MouseEvent) => {
      if (langRef.current && !langRef.current.contains(e.target as Node)) setLangOpen(false);
    };
    document.addEventListener('mousedown', onDoc);
    return () => document.removeEventListener('mousedown', onDoc);
  }, [langOpen]);

  const projects = useQuery({
    queryKey: ['projects'],
    queryFn: () => api.get<ListResp<Project>>('/projects'),
    staleTime: 30_000,
  });
  const current = projects.data?.items?.find((p) => p.id === projectId);

  // dropdowns
  const [open, setOpen] = useState(false);
  const switcherRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (switcherRef.current && !switcherRef.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', onDoc);
    return () => document.removeEventListener('mousedown', onDoc);
  }, [open]);

  const [userOpen, setUserOpen] = useState(false);
  const userRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!userOpen) return;
    const onDoc = (e: MouseEvent) => {
      if (userRef.current && !userRef.current.contains(e.target as Node)) setUserOpen(false);
    };
    document.addEventListener('mousedown', onDoc);
    return () => document.removeEventListener('mousedown', onDoc);
  }, [userOpen]);

  const logout = () => { clearAuth(); nav('/login', { replace: true }); };

  // Force close switcher/user menus when collapsing
  useEffect(() => { if (collapsed) { setOpen(false); setUserOpen(false); } }, [collapsed]);

  const width = collapsed ? 64 : 240;

  return (
    <aside
      className="sticky top-0 self-start h-screen shrink-0 z-20 flex flex-col py-3 pl-3 pr-1
                 transition-[width] duration-200 ease-out"
      style={{ width }}
    >
      <div className="glass glass-edge relative flex flex-col flex-1 min-h-0">
        {/* Brand row + collapse toggle */}
        <div className={'flex items-center pt-3 pb-3 ' + (collapsed ? 'px-2 justify-center' : 'px-4 justify-between')}>
          <Link
            to="/projects"
            className="flex items-center gap-2.5 hover:opacity-90 transition min-w-0"
          >
            <div className="relative w-8 h-8 rounded-xl overflow-hidden grid place-items-center shrink-0
                            bg-gradient-to-br from-indigo-500 via-violet-500 to-cyan-400
                            shadow-[0_2px_8px_-2px_rgba(99,102,241,0.6)]">
              <span className="absolute inset-0 bg-gradient-to-br from-white/40 to-transparent" />
              <span className="relative text-white font-bold text-[12px]">A</span>
            </div>
            {!collapsed && (
              <div className="leading-tight min-w-0">
                <div className="font-bold text-[14px] text-ink tracking-tight">AILens360</div>
              </div>
            )}
          </Link>
          {!collapsed && (
            <button
              onClick={onToggle}
              aria-label={t('nav.collapse')}
              className="w-7 h-7 grid place-items-center rounded-lg text-ink-4 hover:text-ink hover:bg-white/65 transition"
            >
              <IcCollapse />
            </button>
          )}
        </div>

        {/* Expand button when collapsed */}
        {collapsed && (
          <button
            onClick={onToggle}
            aria-label={t('nav.expand')}
            className="mx-auto w-9 h-9 grid place-items-center rounded-xl text-ink-4 hover:text-ink hover:bg-white/65 transition mb-2"
          >
            <IcExpand />
          </button>
        )}

        {/* Nav scroll region */}
        <div className={'flex-1 min-h-0 overflow-y-auto mt-3 pb-3 ' + (collapsed ? 'px-2' : 'px-3')}>
          {inProject ? (
            <ProjectNav
              projectId={projectId}
              current={current}
              open={open}
              setOpen={setOpen}
              switcherRef={switcherRef}
              projects={projects.data?.items ?? []}
              loading={projects.isLoading}
              collapsed={collapsed}
            />
          ) : (
            <RootNav collapsed={collapsed} />
          )}
        </div>

        {/* Language switch */}
        <div ref={langRef} className={'relative ' + (collapsed ? 'px-2 pb-1' : 'px-3 pb-1')}>
          <button
            onClick={() => setLangOpen((v) => !v)}
            className={'w-full flex items-center rounded-xl hover:bg-white/65 transition text-ink-3 hover:text-ink '
                       + (collapsed ? 'justify-center w-9 h-9 mx-auto' : 'gap-2 px-2 py-1.5')}
            aria-label={t('lang.label')}
          >
            <IcGlobe />
            {!collapsed && (
              <>
                <span className="text-[12px] font-medium">{locale === 'zh' ? '中文' : 'English'}</span>
                <span className="ml-auto text-ink-4"><IcChev /></span>
              </>
            )}
          </button>
          {langOpen && (
            <div className={'absolute glass-strong glass-edge p-1.5 z-50 ' +
                            (collapsed ? 'left-[60px] bottom-1 w-[140px]' : 'left-3 right-3 bottom-[44px]')}>
              {(['zh', 'en'] as const).map((l) => (
                <button
                  key={l}
                  onClick={() => { setLocale(l); setLangOpen(false); }}
                  className={'w-full flex items-center justify-between gap-2 px-3 py-1.5 rounded-lg text-[13px] transition-colors ' +
                             (locale === l
                               ? 'bg-indigo-500/10 ring-1 ring-indigo-300/40 font-semibold text-indigo-700'
                               : 'text-ink-2 hover:bg-indigo-50 hover:text-ink')}
                >
                  <span>{t(l === 'zh' ? 'lang.zh' : 'lang.en')}</span>
                  {locale === l && <span className="dot ok" />}
                </button>
              ))}
            </div>
          )}
        </div>

        {/* User pinned bottom */}
        <div ref={userRef} className={'relative pt-2 pb-3 border-t border-[color:var(--glass-line)] '
                                      + (collapsed ? 'px-2' : 'px-3')}>
          <button
            onClick={() => setUserOpen((v) => !v)}
            className={'w-full flex items-center rounded-xl hover:bg-white/65 transition '
                       + (collapsed ? 'justify-center px-1 py-1.5' : 'gap-2.5 px-2 py-2')}
          >
            <div className="w-8 h-8 rounded-full grid place-items-center text-white font-semibold text-[12px]
                            bg-gradient-to-br from-indigo-400 to-violet-500 shrink-0">
              {(username || 'A')[0].toUpperCase()}
            </div>
            {!collapsed && (
              <>
                <div className="min-w-0 flex-1 text-left leading-tight">
                  <div className="text-[12.5px] font-semibold text-ink truncate">{username || 'admin'}</div>
                  <div className="text-[10px] text-ink-4">{t('nav.loggedIn')}</div>
                </div>
                <span className="text-ink-4"><IcChev /></span>
              </>
            )}
          </button>
          {userOpen && (
            <div className={'absolute glass-strong glass-edge p-1.5 z-50 '
                            + (collapsed
                              ? 'left-[60px] bottom-3 w-[160px]'
                              : 'left-3 right-3 bottom-[60px]')}>
              <button
                onClick={logout}
                className="w-full flex items-center gap-2 px-3 py-2 rounded-xl text-[13px]
                           text-rose-700 hover:bg-rose-50/80 transition"
              >
                <IcLogout />
                {t('nav.logout')}
              </button>
            </div>
          )}
        </div>
      </div>
    </aside>
  );
}

/* ── nav modes ───────────────────────────────────────────────── */

function RootNav({ collapsed }: { collapsed: boolean }) {
  const t = useT();
  if (collapsed) {
    return (
      <nav className="flex flex-col gap-1">
        <NavLink to="/projects" end title={t('nav.allProjects')}
          className={({ isActive }) => collapsedItemCls(isActive)}>
          <IcHome />
        </NavLink>
      </nav>
    );
  }
  return (
    <nav className="flex flex-col gap-3">
      <div className="flex flex-col gap-0.5">
        <NavLink to="/projects" end className={({ isActive }) => sideItemCls(isActive)}>
          <IcHome /> <span className="flex-1">{t('nav.allProjects')}</span>
        </NavLink>
      </div>
    </nav>
  );
}

interface ProjectNavProps {
  projectId: string;
  current?: Project;
  loading: boolean;
  projects: Project[];
  open: boolean;
  setOpen: (v: boolean) => void;
  switcherRef: React.RefObject<HTMLDivElement>;
  collapsed: boolean;
}

function ProjectNav({ projectId, current, loading, projects, open, setOpen, switcherRef, collapsed }: ProjectNavProps) {
  const t = useT();
  if (collapsed) {
    return (
      <nav className="flex flex-col gap-2">
        <Link
          to={`/projects/${projectId}/overview`}
          title={current?.name || ''}
          className="mx-auto w-9 h-9 rounded-xl grid place-items-center text-white font-bold text-[12px]
                     bg-gradient-to-br from-indigo-400 via-violet-400 to-cyan-400 mb-1"
        >
          {(current?.name || '?')[0]?.toUpperCase()}
        </Link>
        {projectSections.flatMap((g) => g.items).map((m) => (
          <NavLink
            key={m.to}
            to={`/projects/${projectId}/${m.to}`}
            end={false}
            title={t(m.labelKey)}
            className={({ isActive }) => collapsedItemCls(isActive && !m.soon, m.soon)}
            onClick={(e) => { if (m.soon) e.preventDefault(); }}
          >
            <m.Icon />
          </NavLink>
        ))}
      </nav>
    );
  }

  return (
    <nav className="flex flex-col gap-3">
      <div ref={switcherRef} className="relative">
        <button
          onClick={() => setOpen(!open)}
          className="w-full flex items-center gap-2 px-2.5 py-2 rounded-xl
                     bg-white/85 hover:bg-white border border-white/85 transition text-left"
        >
          <div className="w-7 h-7 rounded-lg grid place-items-center text-white font-bold text-[11px]
                          bg-gradient-to-br from-indigo-400 via-violet-400 to-cyan-400 shrink-0">
            {(current?.name || '?')[0]?.toUpperCase()}
          </div>
          <div className="min-w-0 flex-1">
            <div className="font-semibold text-[12.5px] text-ink truncate">
              {current?.name || (loading ? t('common.loading') : '—')}
            </div>
          </div>
          <span className="text-ink-4 shrink-0"><IcChev /></span>
        </button>

        {open && (
          <div className="absolute left-0 right-0 mt-2 z-50 glass-strong glass-edge p-1.5">
            <div className="px-3 py-1.5 text-[10px] uppercase tracking-[0.16em] text-ink-4 font-semibold">
              {t('nav.switchProject')}
            </div>
            <div className="max-h-72 overflow-auto">
              {projects.map((p) => (
                <Link
                  key={p.id}
                  to={`/projects/${p.id}/overview`}
                  onClick={() => setOpen(false)}
                  className={
                    'flex items-center gap-2 px-2.5 py-1.5 rounded-lg text-[12.5px] transition-colors ' +
                    (p.id === projectId
                      ? 'bg-indigo-500/10 ring-1 ring-indigo-300/40 font-semibold text-indigo-700'
                      : 'text-ink-2 hover:bg-indigo-50 hover:text-ink')
                  }
                >
                  <div className="w-5 h-5 rounded-md grid place-items-center text-white font-bold text-[9px]
                                  bg-gradient-to-br from-indigo-400 via-violet-400 to-cyan-400 shrink-0">
                    {p.name[0].toUpperCase()}
                  </div>
                  <span className="flex-1 truncate">{p.name}</span>
                  {p.id === projectId && <span className="dot ok shrink-0" />}
                </Link>
              ))}
            </div>
            <Link
              to="/projects"
              onClick={() => setOpen(false)}
              className="flex items-center gap-2 px-3 py-2 mt-1 border-t border-[color:var(--glass-line)]
                         text-[12.5px] text-indigo-600 hover:bg-indigo-50/60 rounded-b-xl"
            >
              <IcPlus />
              {t('nav.manageProjects')}
            </Link>
          </div>
        )}
      </div>

      {projectSections.map((g, i) => (
        <div key={i} className="flex flex-col">
          {g.labelKey && (
            <div className="px-2.5 pt-1 pb-1.5 text-[9.5px] tracking-[0.18em] font-bold text-ink-4">
              {t(g.labelKey)}
            </div>
          )}
          <div className="flex flex-col gap-0.5">
            {g.items.map((m) => (
              <NavLink
                key={m.to}
                to={`/projects/${projectId}/${m.to}`}
                end={false}
                className={({ isActive }) => sideItemCls(isActive && !m.soon, m.soon)}
                onClick={(e) => { if (m.soon) e.preventDefault(); }}
              >
                <m.Icon />
                <span className="flex-1">{t(m.labelKey)}</span>
                {m.soon && (
                  <span className="text-[9px] uppercase tracking-[0.14em] text-ink-4
                                   px-1.5 py-0.5 rounded-full bg-white/55 border border-white/70 font-semibold">
                    {t('nav.tag.soon').toLowerCase()}
                  </span>
                )}
              </NavLink>
            ))}
          </div>
        </div>
      ))}
    </nav>
  );
}

function sideItemCls(active: boolean, soon?: boolean) {
  if (soon) return 'flex items-center gap-2 px-2.5 py-1.5 rounded-lg text-[13px] text-ink-4 cursor-not-allowed';
  return (
    'flex items-center gap-2 px-2.5 py-1.5 rounded-lg text-[13px] transition ' +
    (active
      ? 'bg-white/95 text-ink font-semibold shadow-[0_1px_0_rgba(255,255,255,0.7)_inset,_0_2px_8px_-2px_rgba(99,102,241,0.18)]'
      : 'text-ink-3 hover:text-ink hover:bg-white/55 font-medium')
  );
}

function collapsedItemCls(active: boolean, soon?: boolean) {
  if (soon) return 'mx-auto w-9 h-9 grid place-items-center rounded-xl text-ink-4 cursor-not-allowed';
  return (
    'mx-auto w-9 h-9 grid place-items-center rounded-xl transition ' +
    (active
      ? 'bg-white/95 text-ink shadow-[0_1px_0_rgba(255,255,255,0.7)_inset,_0_2px_8px_-2px_rgba(99,102,241,0.18)]'
      : 'text-ink-3 hover:text-ink hover:bg-white/65')
  );
}
