export const fmtNum = (n: number | null | undefined) =>
  (n ?? 0).toLocaleString();

// Money — 2 decimals everywhere by default (KPIs, lists, trace summary).
export const fmtCost = (n: number | null | undefined) =>
  '$' + (n ?? 0).toFixed(2);

// Money — 5 decimals for individual span cost cells, where rounding to 2
// decimals would collapse most values to $0.00.
export const fmtCostFine = (n: number | null | undefined) =>
  '$' + (n ?? 0).toFixed(5);

// Duration in seconds with up to 3 decimals, trailing zeros trimmed.
//   1234 → "1.234s"   5000 → "5s"   1 → "0.001s"   null → "—"
export const fmtDur = (ms: number | null | undefined): string => {
  if (ms == null) return '—';
  const s = (ms / 1000).toFixed(3);
  return parseFloat(s).toString() + 's';
};

// Legacy ms formatter — kept as an alias so existing imports still resolve,
// but now produces the same seconds-based output. Prefer fmtDur in new code.
export const fmtMs = fmtDur;

// Token counts: ≥1m → "1m", ≥1k → "1k", else integer. Up to 2 trailing
// decimals, but no padding zeros ("1k" not "1.00k", "1.5k" not "1.50k").
export const fmtTokens = (n: number | null | undefined): string => {
  const v = Math.round(n ?? 0);
  if (v >= 1_000_000) return shortNum(v / 1_000_000) + 'm';
  if (v >= 1_000)     return shortNum(v / 1_000) + 'k';
  return String(v);
};

function shortNum(v: number): string {
  // 1 → "1", 1.5 → "1.5", 1.234 → "1.23"
  return parseFloat(v.toFixed(2)).toString();
}

export const fmtTsSec = (s: number | null | undefined) =>
  s ? new Date(s * 1000).toLocaleString() : '—';

export const fmtTs = (v: string | number | null | undefined) => {
  if (!v) return '—';
  const d = typeof v === 'number' ? new Date(v) : new Date(v);
  return isNaN(d.getTime()) ? '—' : d.toLocaleString();
};

export async function copyToClipboard(text: string) {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    /* swallow — older browsers */
  }
}

export function prettyJSON(s: string | null | undefined): string {
  if (!s) return '(empty)';
  try { return JSON.stringify(JSON.parse(s), null, 2); }
  catch { return s; }
}
