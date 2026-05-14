import { useState } from 'react';
import { copyToClipboard } from '../lib/fmt';
import { useT } from '../i18n';

// SecretField renders a sensitive value (project_key, etc.) with masked
// display by default, a peek (eye) toggle, and a copy-to-clipboard action.
// Mask preserves first/last few chars so the value is still identifiable
// at a glance (matches the convention of GitHub PAT / Stripe key UIs).
export function SecretField({ value, label }: { value: string; label?: string }) {
  const t = useT();
  const [show, setShow] = useState(false);
  const [copied, setCopied] = useState(false);

  const masked = maskValue(value);

  const doCopy = () => {
    copyToClipboard(value);
    setCopied(true);
    setTimeout(() => setCopied(false), 1400);
  };

  return (
    <div className="rounded-2xl bg-white/55 border border-white/70 px-3.5 py-2">
      {label && (
        <div className="text-[10px] uppercase tracking-[0.14em] text-ink-4 font-semibold mb-0.5">
          {label}
        </div>
      )}
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={doCopy}
          title={copied ? t('common.copied') : t('secret.clickToCopy')}
          className="mono text-[11.5px] flex-1 min-w-0 truncate text-left
                     hover:text-ink-2 transition cursor-pointer"
        >
          {show ? value : masked}
        </button>
        <button
          type="button"
          onClick={() => setShow((s) => !s)}
          title={show ? t('secret.hide') : t('secret.show')}
          className="shrink-0 inline-flex items-center justify-center w-7 h-7 rounded-full
                     bg-white/85 hover:bg-white text-ink-3 hover:text-ink
                     border border-white/90 shadow-sm transition"
        >
          {show ? <EyeOffIcon /> : <EyeIcon />}
        </button>
        <button
          type="button"
          onClick={doCopy}
          className="shrink-0 px-3 py-1 rounded-full bg-white/95 text-xs font-semibold
                     text-ink-2 hover:bg-white shadow-sm transition"
        >
          {copied ? t('common.copied') : t('common.copy')}
        </button>
      </div>
    </div>
  );
}

function maskValue(v: string) {
  if (!v) return '';
  if (v.length <= 12) return '•'.repeat(v.length);
  return v.slice(0, 4) + '•'.repeat(Math.max(8, v.length - 8)) + v.slice(-4);
}

function EyeIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor"
         strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8S1 12 1 12z" />
      <circle cx="12" cy="12" r="3" />
    </svg>
  );
}

function EyeOffIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor"
         strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M17.94 17.94A10.94 10.94 0 0 1 12 20c-7 0-11-8-11-8a19.77 19.77 0 0 1 5.06-5.94" />
      <path d="M9.9 4.24A10.94 10.94 0 0 1 12 4c7 0 11 8 11 8a19.86 19.86 0 0 1-3.17 4.19" />
      <path d="M14.12 14.12A3 3 0 1 1 9.88 9.88" />
      <line x1="1" y1="1" x2="23" y2="23" />
    </svg>
  );
}
