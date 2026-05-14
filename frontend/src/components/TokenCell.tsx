import { useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { fmtNum, fmtTokens } from '../lib/fmt';
import { useT } from '../i18n';

// TokenCell unifies how token counts are surfaced across the app. Different
// surfaces (trace list, span list, summary tiles) historically mixed
// "TotalTokens" with "InputTokens+OutputTokens", which diverge whenever the
// upstream charged for cached input (OpenAI normalizes InputTokens to the
// uncached portion). Always rendering the same breakdown — and exposing the
// full split via a hover popover — keeps the displayed numbers consistent
// across views and makes the cache contribution visible.

export interface TokenStats {
  input?: number;
  output?: number;
  cached?: number;        // CachedInputTokens — input served from cache
  cacheCreate?: number;   // CacheCreationInputTokens — Anthropic cache writes
  reasoning?: number;     // ReasoningTokens (OpenAI/Gemini: separate from output)
  total?: number;         // explicit total when provided by upstream; falls back to a derived sum
  estimated?: boolean;
}

interface Props {
  tokens: TokenStats;
  align?: 'left' | 'right';
  // sm = single inline row (no wrap, for tight cells like span rows)
  // md = two-line stack with cache below (table cell / summary tile)
  size?: 'sm' | 'md';
  className?: string;
}

export function TokenCell({ tokens, align = 'right', size = 'md', className = '' }: Props) {
  const t = useT();
  const { input = 0, output = 0, cached = 0, cacheCreate = 0, reasoning = 0, estimated } = tokens;
  const hasCache = cached > 0 || cacheCreate > 0;
  const derivedTotal = input + output + cached + cacheCreate;
  const total = tokens.total && tokens.total > 0 ? tokens.total : derivedTotal;

  const alignCls = align === 'right' ? 'items-end text-right' : 'items-start text-left';

  const anchorRef = useRef<HTMLDivElement | null>(null);
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState<{ top: number; left: number; right?: number } | null>(null);

  // Compute the tooltip's viewport position from the anchor's bounding rect.
  // Using a portal + fixed positioning is necessary because parent containers
  // (table wrappers, glass cards) clip overflow.
  useLayoutEffect(() => {
    if (!open || !anchorRef.current) return;
    const update = () => {
      const r = anchorRef.current!.getBoundingClientRect();
      const top = r.bottom + 6;
      if (align === 'right') {
        setPos({ top, left: 0, right: window.innerWidth - r.right });
      } else {
        setPos({ top, left: r.left });
      }
    };
    update();
    window.addEventListener('scroll', update, true);
    window.addEventListener('resize', update);
    return () => {
      window.removeEventListener('scroll', update, true);
      window.removeEventListener('resize', update);
    };
  }, [open, align]);

  return (
    <div
      ref={anchorRef}
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => setOpen(false)}
      onFocus={() => setOpen(true)}
      onBlur={() => setOpen(false)}
      className={`relative inline-flex flex-col gap-0.5 ${alignCls} ${className}`}
    >
      <div className="inline-flex items-center gap-2 tnum mono text-[12px] leading-none whitespace-nowrap">
        <span className="inline-flex items-center gap-0.5 text-emerald-600">
          <ArrowDown />
          <span>{fmtTokens(input)}</span>
        </span>
        <span className="inline-flex items-center gap-0.5 text-violet-600">
          <ArrowUp />
          <span>{fmtTokens(output)}</span>
        </span>
        {size === 'sm' && hasCache && (
          <span className="inline-flex items-center gap-0.5 text-ink-3">
            <CacheIcon />
            <span>{fmtTokens(cached + cacheCreate)}</span>
          </span>
        )}
        {estimated && <span className="text-[9px] uppercase tracking-wider text-ink-4">est</span>}
      </div>

      {size === 'md' && hasCache && (
        <div className="inline-flex items-center gap-0.5 tnum mono text-[11px] leading-none text-ink-3 whitespace-nowrap">
          <CacheIcon />
          <span>{fmtTokens(cached + cacheCreate)}</span>
        </div>
      )}

      {open && pos && createPortal(
        <div
          style={{
            position: 'fixed',
            top: pos.top,
            left: align === 'left' ? pos.left : undefined,
            right: align === 'right' ? pos.right : undefined,
            zIndex: 9999,
          }}
          className="pointer-events-none min-w-[200px] rounded-xl bg-slate-900/95 text-slate-100
                     border border-slate-700/60 shadow-xl backdrop-blur px-3.5 py-2.5 text-left"
        >
          <div className="text-[11px] font-semibold mb-1.5 text-slate-200">
            {t('token.tip.title')}
          </div>
          <Row label={t('token.tip.input')}  value={input} />
          <Row label={t('token.tip.output')} value={output} />
          {cached > 0 && (
            <Row label={t('token.tip.cacheRead')} value={cached} dim />
          )}
          {cacheCreate > 0 && (
            <Row label={t('token.tip.cacheWrite')} value={cacheCreate} dim />
          )}
          {reasoning > 0 && (
            <Row label={t('token.tip.reasoning')} value={reasoning} dim />
          )}
          <div className="my-1.5 h-px bg-slate-700/70" />
          <Row label={t('token.tip.total')} value={total} bold />
        </div>,
        document.body,
      )}
    </div>
  );
}

function Row({ label, value, bold, dim }: { label: string; value: number; bold?: boolean; dim?: boolean }) {
  return (
    <div className="flex items-center justify-between gap-6 py-0.5">
      <span className={`text-[11.5px] ${dim ? 'text-slate-400' : 'text-slate-300'}`}>{label}</span>
      <span className={`tnum mono text-[12px] ${bold ? 'font-semibold text-white' : 'text-slate-100'}`}>
        {fmtNum(value)}
      </span>
    </div>
  );
}

function ArrowDown() {
  return (
    <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor"
         strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 5v14M19 12l-7 7-7-7" />
    </svg>
  );
}

function ArrowUp() {
  return (
    <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor"
         strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 19V5M5 12l7-7 7 7" />
    </svg>
  );
}

function CacheIcon() {
  return (
    <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor"
         strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="3" y="6" width="18" height="14" rx="2" />
      <path d="M3 10h18M8 6V4M16 6V4" />
    </svg>
  );
}
