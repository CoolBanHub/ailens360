import { FormEvent, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api } from '../lib/api';
import { setAuth } from '../lib/auth';
import { useI18n, useT } from '../i18n';

interface LoginResp { token: string; expires_at: number; username: string; }

export default function Login() {
  const nav = useNavigate();
  const t = useT();
  const { locale, setLocale } = useI18n();
  const [u, setU] = useState('');
  const [p, setP] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const r = await api.post<LoginResp>('/auth/login', { username: u, password: p });
      setAuth(r.token, r.username);
      nav('/', { replace: true });
    } catch (e: any) {
      setErr(e.message || t('login.errorFallback'));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center px-4 relative">
      {/* Top-right language switch */}
      <div
        role="tablist"
        aria-label={t('lang.label')}
        className="absolute top-5 right-5 inline-flex items-center p-0.5 rounded-full
                   bg-white/65 ring-1 ring-[color:var(--glass-line)]
                   backdrop-blur-sm shadow-[0_1px_2px_rgba(15,23,42,0.04)]"
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
                'px-3 py-1 rounded-full text-[11.5px] font-semibold tracking-[0.04em] transition-colors duration-200 ' +
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

      <div className="w-full max-w-[420px] relative reveal r-1">
        <div className="glass glass-edge relative p-8">
          {/* logo + brand */}
          <div className="flex items-center gap-3 mb-7">
            <div className="relative w-11 h-11 rounded-2xl overflow-hidden grid place-items-center
                            bg-gradient-to-br from-indigo-500 via-violet-500 to-cyan-400
                            shadow-[0_4px_14px_-2px_rgba(99,102,241,0.5)]">
              <span className="absolute inset-0 bg-gradient-to-br from-white/45 to-transparent" />
              <span className="relative text-white font-bold text-base">A</span>
            </div>
            <div className="leading-tight">
              <div className="font-bold text-lg tracking-tight">AILens360</div>
              <div className="text-[10px] uppercase tracking-[0.18em] text-ink-4 font-medium">
                LLM Observability
              </div>
            </div>
          </div>

          <h1 className="text-[28px] font-bold tracking-tight leading-tight mb-1">
            {t('login.welcome')} <span className="grad-text">{t('login.welcomeRole')}</span>
          </h1>
          <p className="text-sm text-ink-3 mb-7">{t('login.subtitle')}</p>

          <form onSubmit={submit} className="flex flex-col gap-4">
            <label className="block">
              <span className="text-xs font-semibold text-ink-3 mb-1.5 block">{t('login.username')}</span>
              <input
                value={u}
                onChange={(e) => setU(e.target.value)}
                autoComplete="username"
                className="w-full bg-white/70 border border-white/70 rounded-2xl
                           px-4 py-3 text-sm text-ink placeholder:text-ink-4
                           focus:bg-white focus:border-indigo-200 outline-none transition"
                required
              />
            </label>
            <label className="block">
              <span className="text-xs font-semibold text-ink-3 mb-1.5 block">{t('login.password')}</span>
              <input
                type="password"
                value={p}
                onChange={(e) => setP(e.target.value)}
                autoComplete="current-password"
                className="w-full bg-white/70 border border-white/70 rounded-2xl
                           px-4 py-3 text-sm text-ink placeholder:text-ink-4
                           focus:bg-white focus:border-indigo-200 outline-none transition"
                required
              />
            </label>

            {err && (
              <div className="flex items-start gap-2 px-3 py-2.5 rounded-2xl
                              bg-rose-50/80 border border-rose-200 text-rose-700 text-xs">
                <span className="dot err mt-1.5" />
                <span>{err}</span>
              </div>
            )}

            <button type="submit" disabled={busy} className="btn-grad justify-center mt-1">
              {busy ? (
                <>
                  <span className="w-3 h-3 rounded-full border-2 border-white/40 border-t-white animate-spin" />
                  {t('login.submitting')}
                </>
              ) : (
                <>
                  {t('login.submit')}
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                       strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M5 12h14M13 5l7 7-7 7"/>
                  </svg>
                </>
              )}
            </button>
          </form>
        </div>
      </div>
    </div>
  );
}
