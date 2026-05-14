import { useState } from 'react';
import type { MouseEvent } from 'react';
import { copyToClipboard } from '../lib/fmt';
import { useT } from '../i18n';

// Inline mono text that copies its value on click. Used for user / session /
// trace IDs in lists and headers. stopPropagation lets it sit inside clickable
// rows without triggering the row action. A small copy icon fades in on hover;
// a green tick flashes for ~1.2s on success.
export function CopyableId({ value, className = '' }: { value: string; className?: string }) {
  const t = useT();
  const [copied, setCopied] = useState(false);
  const onClick = (e: MouseEvent) => {
    e.stopPropagation();
    e.preventDefault();
    copyToClipboard(value);
    setCopied(true);
    setTimeout(() => setCopied(false), 1200);
  };
  return (
    <button
      type="button"
      onClick={onClick}
      title={copied ? t('common.copied') : `${value}\n${t('secret.clickToCopy')}`}
      className={
        'group inline-flex items-center gap-1 mono max-w-full align-middle ' +
        'hover:text-ink transition ' + className
      }
    >
      <span className="truncate">{value}</span>
      {copied ? (
        <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor"
             strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"
             className="shrink-0 text-emerald-600">
          <path d="M5 13l4 4L19 7"/>
        </svg>
      ) : (
        <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor"
             strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"
             className="shrink-0 opacity-0 group-hover:opacity-70 transition-opacity">
          <rect x="9" y="9" width="13" height="13" rx="2"/>
          <path d="M5 15V5a2 2 0 0 1 2-2h10"/>
        </svg>
      )}
    </button>
  );
}
