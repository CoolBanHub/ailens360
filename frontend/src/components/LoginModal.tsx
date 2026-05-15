import { FormEvent, useEffect, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { api } from '../lib/api';
import { setAuth } from '../lib/auth';
import { useT } from '../i18n';

interface LoginResp { token: string; expires_at: number; username: string; }

interface Props {
  open: boolean;
  onClose: () => void;
  /** Where to go after success. Falls back to location.state.from or /projects. */
  redirectTo?: string;
}

export default function LoginModal({ open, onClose, redirectTo }: Props) {
  const nav = useNavigate();
  const loc = useLocation();
  const t = useT();
  const [u, setU] = useState('');
  const [p, setP] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  // ESC to close + lock body scroll while open
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    document.addEventListener('keydown', onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.removeEventListener('keydown', onKey);
      document.body.style.overflow = prev;
    };
  }, [open, onClose]);

  // Reset transient state when closed so the next open starts clean
  useEffect(() => {
    if (!open) { setErr(null); setBusy(false); }
  }, [open]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const r = await api.post<LoginResp>('/auth/login', { username: u, password: p });
      setAuth(r.token, r.username);
      const rawFrom = (loc.state as { from?: string } | null)?.from;
      // Never bounce back to the landing or login URL after a successful login.
      const from = rawFrom && rawFrom !== '/' && !rawFrom.startsWith('/login') ? rawFrom : null;
      nav(redirectTo || from || '/projects', { replace: true });
    } catch (e: any) {
      setErr(e.message || t('login.errorFallback'));
    } finally {
      setBusy(false);
    }
  }

  if (!open) return null;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="login-modal-title"
      className="fixed inset-0 z-[60] flex items-center justify-center px-4 modal-fade"
    >
      {/* backdrop */}
      <button
        type="button"
        aria-label="Close"
        onClick={onClose}
        className="absolute inset-0 bg-slate-900/55 backdrop-blur-[14px] cursor-default"
      />

      {/* card — solid white so behind text doesn't bleed through */}
      <div className="relative w-full max-w-[420px] modal-pop">
        <div className="relative p-7 rounded-[22px] bg-white border border-white/90
                        shadow-[0_1px_0_rgba(255,255,255,0.9)_inset,_0_2px_8px_rgba(15,23,42,0.06),_0_30px_60px_-12px_rgba(15,23,42,0.35)]">
          {/* close button */}
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="absolute top-3.5 right-3.5 w-8 h-8 grid place-items-center rounded-full
                       text-ink-4 hover:text-ink hover:bg-white/65 transition"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                 strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
              <path d="M18 6L6 18M6 6l12 12"/>
            </svg>
          </button>

          {/* logo + brand */}
          <div className="flex items-center gap-3 mb-6">
            <div className="relative w-10 h-10 rounded-2xl overflow-hidden grid place-items-center
                            bg-gradient-to-br from-indigo-500 via-violet-500 to-cyan-400
                            shadow-[0_4px_14px_-2px_rgba(99,102,241,0.5)]">
              <span className="absolute inset-0 bg-gradient-to-br from-white/45 to-transparent" />
              <span className="relative text-white font-bold text-[14px]">A</span>
            </div>
            <div className="leading-tight">
              <div className="font-bold text-[15px] tracking-tight">AILens360</div>
              <div className="text-[10px] uppercase tracking-[0.18em] text-ink-4 font-medium">
                LLM Observability
              </div>
            </div>
          </div>

          <h1 id="login-modal-title" className="text-[24px] font-bold tracking-tight leading-tight mb-1">
            {t('login.welcome')} <span className="grad-text">{t('login.welcomeRole')}</span>
          </h1>
          <p className="text-[13px] text-ink-3 mb-6">{t('login.subtitle')}</p>

          <form onSubmit={submit} className="flex flex-col gap-3.5">
            <label className="block">
              <span className="text-[11px] font-semibold text-ink-3 mb-1.5 block">{t('login.username')}</span>
              <input
                value={u}
                onChange={(e) => setU(e.target.value)}
                autoComplete="username"
                autoFocus
                className="w-full bg-white/75 border border-white/70 rounded-2xl
                           px-4 py-2.5 text-sm text-ink placeholder:text-ink-4
                           focus:bg-white focus:border-indigo-200 outline-none transition"
                required
              />
            </label>
            <label className="block">
              <span className="text-[11px] font-semibold text-ink-3 mb-1.5 block">{t('login.password')}</span>
              <input
                type="password"
                value={p}
                onChange={(e) => setP(e.target.value)}
                autoComplete="current-password"
                className="w-full bg-white/75 border border-white/70 rounded-2xl
                           px-4 py-2.5 text-sm text-ink placeholder:text-ink-4
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
