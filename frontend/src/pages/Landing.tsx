import { useEffect, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import LoginModal from '../components/LoginModal';
import { useI18n, useT } from '../i18n';
import { useAuthed } from '../lib/auth';

const GITHUB_URL = 'https://github.com/CoolBanHub/ailens360';

export default function Landing() {
  const t = useT();
  const { locale, setLocale } = useI18n();
  const authed = useAuthed();
  const loc = useLocation();
  const nav = useNavigate();
  const [loginOpen, setLoginOpen] = useState(false);

  // Sync modal with URL: visiting /login opens it (deep link / RequireAuth bounce),
  // any other path closes it. Authed users on /login skip straight to /projects.
  useEffect(() => {
    if (loc.pathname === '/login') {
      if (authed) {
        nav('/projects', { replace: true });
        setLoginOpen(false);
      } else {
        setLoginOpen(true);
      }
    } else {
      setLoginOpen(false);
    }
  }, [loc.pathname, authed, nav]);

  function openConsole(e: React.MouseEvent) {
    e.preventDefault();
    if (authed) {
      nav('/projects');
      return;
    }
    // Push /login into history so Back closes the modal naturally.
    // Only pass `from` when it's a real protected page; opening from the
    // landing itself would otherwise cause login success to land back here
    // instead of going to /projects.
    const here = loc.pathname + loc.search;
    const isLandingish = loc.pathname === '/' || loc.pathname === '/login';
    nav('/login', isLandingish ? undefined : { state: { from: here } });
  }

  function closeLogin() {
    setLoginOpen(false);
    if (loc.pathname === '/login') {
      nav('/', { replace: true });
    }
  }

  return (
    <div className="relative min-h-screen overflow-hidden">
      {/* Extra decorative orbs just for the landing surface — sits above the
          global single-orb #bg-aura but stays well behind content. */}
      <div className="pointer-events-none absolute inset-0 -z-10 overflow-hidden">
        <div className="absolute -top-40 -right-32 w-[620px] h-[620px] rounded-full blur-[140px] opacity-25
                        bg-gradient-to-br from-violet-300 via-indigo-300 to-cyan-200" />
        <div className="absolute top-[55%] -left-40 w-[520px] h-[520px] rounded-full blur-[140px] opacity-20
                        bg-gradient-to-tr from-cyan-200 via-sky-200 to-indigo-200" />
      </div>

      {/* Top bar */}
      <header className="relative z-10">
        <div className="max-w-6xl mx-auto px-6 pt-6 pb-2 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="relative w-10 h-10 rounded-2xl overflow-hidden grid place-items-center
                            bg-gradient-to-br from-indigo-500 via-violet-500 to-cyan-400
                            shadow-[0_4px_14px_-2px_rgba(99,102,241,0.5)]">
              <span className="absolute inset-0 bg-gradient-to-br from-white/45 to-transparent" />
              <span className="relative text-white font-bold text-base">A</span>
            </div>
            <div className="leading-tight">
              <div className="font-bold text-[15px] tracking-tight">AILens360</div>
              <div className="text-[9.5px] uppercase tracking-[0.2em] text-ink-4 font-medium">
                LLM Observability
              </div>
            </div>
          </div>

          <nav className="hidden md:flex items-center gap-1 px-2 py-1.5 rounded-full
                          bg-white/55 ring-1 ring-[color:var(--glass-line)] backdrop-blur-sm">
            <a href="#features" className="nav-pill !text-[12.5px]">{t('landing.nav.features')}</a>
            <a href="#how"      className="nav-pill !text-[12.5px]">{t('landing.nav.howItWorks')}</a>
            <a href={GITHUB_URL} target="_blank" rel="noreferrer" className="nav-pill !text-[12.5px]">
              {t('landing.nav.github')}
            </a>
          </nav>

          <div className="flex items-center gap-2">
            <div
              role="tablist"
              aria-label={t('lang.label')}
              className="inline-flex items-center p-0.5 rounded-full
                         bg-white/65 ring-1 ring-[color:var(--glass-line)] backdrop-blur-sm"
            >
              {(['zh', 'en'] as const).map((l) => {
                const active = locale === l;
                return (
                  <button
                    key={l}
                    role="tab"
                    aria-selected={active}
                    onClick={() => setLocale(l)}
                    className={
                      'px-2.5 py-1 rounded-full text-[11px] font-semibold tracking-[0.04em] transition-colors ' +
                      (active
                        ? 'bg-gradient-to-r from-[var(--grad-1)] via-[var(--grad-2)] to-[var(--grad-3)] text-white shadow-[0_4px_14px_-4px_rgba(99,102,241,0.55)]'
                        : 'text-ink-4 hover:text-ink-2')
                    }
                  >
                    {t(l === 'zh' ? 'lang.zh' : 'lang.en')}
                  </button>
                );
              })}
            </div>
            <button type="button" onClick={openConsole} className="btn-grad !py-2 !px-4 !text-[13px]">
              {authed ? t('landing.nav.openConsole') : t('landing.nav.signin')}
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                   strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
                <path d="M5 12h14M13 5l7 7-7 7"/>
              </svg>
            </button>
          </div>
        </div>
      </header>

      {/* Hero */}
      <section className="relative z-10 max-w-6xl mx-auto px-6 pt-14 md:pt-20 pb-20">
        <div className="grid md:grid-cols-12 gap-10 items-center">
          <div className="md:col-span-7 reveal r-1">
            <div className="inline-flex items-center gap-2 pill-ring mb-6">
              <span className="live-dot" />
              <span>{t('landing.hero.badge')}</span>
            </div>
            <h1 className="text-[44px] md:text-[58px] font-bold tracking-tight leading-[1.05]">
              {t('landing.hero.title1')}
              <br />
              <span className="grad-text">{t('landing.hero.title2')}</span>
            </h1>
            <p className="mt-5 text-[15.5px] text-ink-3 leading-relaxed max-w-[560px]">
              {t('landing.hero.subtitle')}
            </p>

            <div className="mt-8 flex flex-wrap items-center gap-3">
              <button type="button" onClick={openConsole} className="btn-grad">
                {t('landing.hero.cta')}
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                     strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M5 12h14M13 5l7 7-7 7"/>
                </svg>
              </button>
              <a href={GITHUB_URL} target="_blank" rel="noreferrer" className="btn-ghost">
                <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor">
                  <path d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.1.79-.25.79-.56 0-.27-.01-1-.02-1.97-3.2.7-3.88-1.54-3.88-1.54-.52-1.33-1.28-1.69-1.28-1.69-1.05-.71.08-.7.08-.7 1.16.08 1.78 1.19 1.78 1.19 1.03 1.76 2.7 1.25 3.36.95.1-.74.4-1.25.73-1.54-2.55-.29-5.24-1.28-5.24-5.7 0-1.26.45-2.29 1.19-3.09-.12-.29-.52-1.47.11-3.06 0 0 .97-.31 3.18 1.18a11 11 0 0 1 5.8 0c2.21-1.49 3.18-1.18 3.18-1.18.63 1.59.23 2.77.11 3.06.74.8 1.19 1.83 1.19 3.09 0 4.43-2.7 5.4-5.27 5.69.41.35.78 1.05.78 2.12 0 1.53-.01 2.77-.01 3.15 0 .31.21.67.8.56A11.5 11.5 0 0 0 23.5 12c0-6.35-5.15-11.5-11.5-11.5Z"/>
                </svg>
                {t('landing.hero.ctaGithub')}
              </a>
            </div>

            <div className="mt-10 grid grid-cols-3 gap-4 max-w-[560px]">
              <Stat label={t('landing.hero.statTraces')} value="1.2B+" tone="grad" />
              <Stat label={t('landing.hero.statSDK')} value={t('landing.hero.statSDKVal')} tone="ink" />
              <Stat label={t('landing.hero.statSelf')} value={t('landing.hero.statSelfVal')} tone="ink" />
            </div>
          </div>

          {/* Mock console card */}
          <div className="md:col-span-5 reveal r-3">
            <div className="relative">
              <div className="absolute -inset-3 rounded-[28px] bg-gradient-to-br from-indigo-300/30 via-violet-300/20 to-cyan-200/30 blur-2xl" />
              <div className="glass glass-edge relative p-5">
                <div className="flex items-center justify-between mb-4">
                  <div className="flex items-center gap-1.5">
                    <span className="w-2.5 h-2.5 rounded-full bg-rose-300/70" />
                    <span className="w-2.5 h-2.5 rounded-full bg-amber-300/70" />
                    <span className="w-2.5 h-2.5 rounded-full bg-emerald-300/70" />
                  </div>
                  <div className="text-[10.5px] uppercase tracking-[0.18em] text-ink-4 font-semibold">
                    ailens360 / console
                  </div>
                </div>

                <div className="codeblock-frame">
                  <div className="codeblock-header">
                    <span>OPENAI · NODE</span>
                    <span className="text-emerald-300/90">{t('landing.hero.codeHint')}</span>
                  </div>
                  <pre>{`import OpenAI from "openai";

const client = new OpenAI({
  baseURL: "https://ailens.your.co/`}<span style={{ color: '#a5f3fc' }}>{`https://api.openai.com/v1`}</span>{`",
  apiKey:  process.env.OPENAI_API_KEY,
  defaultHeaders: {
    "X-AILens-Project-Key": "ailens_p_•••"
  }
});`}</pre>
                </div>

                {/* mini KPI strip */}
                <div className="mt-4 grid grid-cols-3 gap-2">
                  <MiniKPI label="latency" value="412ms" />
                  <MiniKPI label="tokens" value="3.4k" />
                  <MiniKPI label="cost" value="$0.018" />
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* Features */}
      <section id="features" className="relative z-10 max-w-6xl mx-auto px-6 pb-24">
        <div className="text-center mb-12 reveal r-1">
          <h2 className="text-[30px] md:text-[36px] font-bold tracking-tight">
            {t('landing.feature.title')}
          </h2>
          <p className="mt-3 text-ink-3 max-w-2xl mx-auto text-[14.5px]">
            {t('landing.feature.subtitle')}
          </p>
        </div>

        <div className="grid md:grid-cols-3 gap-5">
          <Feature
            delay="r-1"
            icon={<IconPlug />}
            title={t('landing.feature.zero.title')}
            body={t('landing.feature.zero.body')}
          />
          <Feature
            delay="r-2"
            icon={<IconTrace />}
            title={t('landing.feature.full.title')}
            body={t('landing.feature.full.body')}
          />
          <Feature
            delay="r-3"
            icon={<IconChart />}
            title={t('landing.feature.cost.title')}
            body={t('landing.feature.cost.body')}
          />
          <Feature
            delay="r-2"
            icon={<IconShield />}
            title={t('landing.feature.privacy.title')}
            body={t('landing.feature.privacy.body')}
          />
          <Feature
            delay="r-3"
            icon={<IconStream />}
            title={t('landing.feature.stream.title')}
            body={t('landing.feature.stream.body')}
          />
          <Feature
            delay="r-4"
            icon={<IconStack />}
            title={t('landing.feature.stack.title')}
            body={t('landing.feature.stack.body')}
          />
        </div>
      </section>

      {/* How it works */}
      <section id="how" className="relative z-10 max-w-6xl mx-auto px-6 pb-24">
        <div className="text-center mb-12 reveal r-1">
          <h2 className="text-[30px] md:text-[36px] font-bold tracking-tight">
            {t('landing.how.title')}
          </h2>
          <p className="mt-3 text-ink-3 max-w-2xl mx-auto text-[14.5px]">
            {t('landing.how.subtitle')}
          </p>
        </div>

        <div className="grid md:grid-cols-3 gap-5">
          <Step
            delay="r-1"
            tag={t('landing.how.step1.tag')}
            title={t('landing.how.step1.title')}
            body={t('landing.how.step1.body')}
          />
          <Step
            delay="r-2"
            tag={t('landing.how.step2.tag')}
            title={t('landing.how.step2.title')}
            body={t('landing.how.step2.body')}
          />
          <Step
            delay="r-3"
            tag={t('landing.how.step3.tag')}
            title={t('landing.how.step3.title')}
            body={t('landing.how.step3.body')}
          />
        </div>

        <div className="mt-16 flex justify-center reveal r-4">
          <button type="button" onClick={openConsole} className="btn-grad !py-3 !px-6">
            {t('landing.hero.cta')}
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                 strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
              <path d="M5 12h14M13 5l7 7-7 7"/>
            </svg>
          </button>
        </div>
      </section>

      {/* Footer */}
      <footer className="relative z-10 border-t border-[color:var(--glass-line)]">
        <div className="max-w-6xl mx-auto px-6 py-8 flex flex-col md:flex-row items-center justify-between gap-3">
          <div className="flex items-center gap-2 text-[12.5px] text-ink-4">
            <span className="w-1.5 h-1.5 rounded-full bg-gradient-to-r from-indigo-500 to-cyan-400" />
            {t('landing.footer.tag')}
          </div>
          <div className="text-[12px] text-ink-4 tracking-wide">
            {t('landing.footer.copy')}
          </div>
        </div>
      </footer>

      <LoginModal open={loginOpen} onClose={closeLogin} />
    </div>
  );
}

/* ── small subcomponents ──────────────────────────────────────────── */

function Stat({ label, value, tone }: { label: string; value: string; tone: 'grad' | 'ink' }) {
  return (
    <div>
      <div className={
        'kpi-num text-[22px] md:text-[26px] leading-tight ' +
        (tone === 'grad' ? 'grad-text' : 'text-ink')
      }>
        {value}
      </div>
      <div className="mt-1 text-[11px] uppercase tracking-[0.14em] text-ink-4 font-semibold">
        {label}
      </div>
    </div>
  );
}

function MiniKPI({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-2xl bg-white/60 ring-1 ring-[color:var(--glass-line)] px-3 py-2.5">
      <div className="text-[10px] uppercase tracking-[0.14em] text-ink-4 font-semibold">{label}</div>
      <div className="kpi-num text-[15px] text-ink mt-0.5">{value}</div>
    </div>
  );
}

function Feature({
  delay, icon, title, body,
}: {
  delay: string; icon: React.ReactNode; title: string; body: string;
}) {
  return (
    <div className={`glass glass-edge p-6 reveal ${delay} relative group`}>
      <div className="w-11 h-11 rounded-2xl grid place-items-center mb-4
                      bg-gradient-to-br from-indigo-500/95 via-violet-500/95 to-cyan-400/95
                      text-white shadow-[0_8px_22px_-6px_rgba(99,102,241,0.55)]">
        {icon}
      </div>
      <div className="font-semibold text-[15.5px] text-ink tracking-tight">{title}</div>
      <p className="mt-2 text-[13.5px] text-ink-3 leading-relaxed">{body}</p>
    </div>
  );
}

function Step({
  delay, tag, title, body,
}: {
  delay: string; tag: string; title: string; body: string;
}) {
  return (
    <div className={`glass glass-edge p-6 reveal ${delay} relative`}>
      <div className="absolute -top-3 left-6 px-2.5 py-1 rounded-full
                      bg-gradient-to-r from-indigo-500 via-violet-500 to-cyan-400 text-white
                      text-[10.5px] font-bold tracking-[0.18em] shadow-[0_6px_18px_-4px_rgba(99,102,241,0.6)]">
        {tag}
      </div>
      <div className="font-semibold text-[15.5px] text-ink tracking-tight mt-2">{title}</div>
      <p className="mt-2 text-[13.5px] text-ink-3 leading-relaxed">{body}</p>
    </div>
  );
}

/* ── icons (inline, no external dep) ──────────────────────────────── */

const iconProps = {
  width: 20,
  height: 20,
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 2,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
};

function IconPlug() {
  return (
    <svg {...iconProps}>
      <path d="M9 2v6M15 2v6M7 8h10v3a5 5 0 0 1-10 0V8ZM12 16v6"/>
    </svg>
  );
}
function IconTrace() {
  return (
    <svg {...iconProps}>
      <path d="M4 6h6M14 6h6M4 12h10M18 12h2M4 18h4M12 18h8"/>
      <circle cx="11" cy="6" r="1.4"/>
      <circle cx="15" cy="12" r="1.4"/>
      <circle cx="9" cy="18" r="1.4"/>
    </svg>
  );
}
function IconChart() {
  return (
    <svg {...iconProps}>
      <path d="M3 3v18h18"/>
      <path d="M7 15l4-4 3 3 5-6"/>
    </svg>
  );
}
function IconShield() {
  return (
    <svg {...iconProps}>
      <path d="M12 3l8 3v6c0 5-3.5 8.5-8 9-4.5-.5-8-4-8-9V6l8-3Z"/>
      <path d="m9 12 2 2 4-4"/>
    </svg>
  );
}
function IconStream() {
  return (
    <svg {...iconProps}>
      <path d="M3 8c4 0 4-3 8-3s4 3 8 3"/>
      <path d="M3 14c4 0 4-3 8-3s4 3 8 3"/>
      <path d="M3 20c4 0 4-3 8-3s4 3 8 3"/>
    </svg>
  );
}
function IconStack() {
  return (
    <svg {...iconProps}>
      <path d="M12 3 3 8l9 5 9-5-9-5Z"/>
      <path d="m3 13 9 5 9-5"/>
      <path d="m3 18 9 5 9-5"/>
    </svg>
  );
}
