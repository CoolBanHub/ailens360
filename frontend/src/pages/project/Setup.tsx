import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useParams } from 'react-router-dom';
import { useState } from 'react';
import { api } from '../../lib/api';
import type { ListResp, Project } from '../../lib/types';
import { copyToClipboard } from '../../lib/fmt';
import { SecretField } from '../../components/SecretField';
import { useT } from '../../i18n';

const presets: { label: string; key: keyof Project['example']; grad: string; }[] = [
  { label: 'OpenAI',    key: 'openai',    grad: 'from-emerald-300 to-teal-400' },
  { label: 'Anthropic', key: 'anthropic', grad: 'from-orange-300 to-rose-400' },
  { label: 'Gemini',    key: 'gemini',    grad: 'from-sky-300 to-indigo-400' },
];

export default function ProjectSetup() {
  const { projectId = '' } = useParams();
  const qc = useQueryClient();
  const t = useT();
  const { data } = useQuery({
    queryKey: ['projects'],
    queryFn: () => api.get<ListResp<Project>>('/projects'),
    staleTime: 30_000,
  });
  const p = data?.items?.find((x) => x.id === projectId);

  const [copied, setCopied] = useState<string | null>(null);
  const copy = (key: string, text: string) => {
    copyToClipboard(text);
    setCopied(key);
    setTimeout(() => setCopied((v) => (v === key ? null : v)), 1400);
  };

  const [confirmReset, setConfirmReset] = useState(false);
  const resetKey = useMutation({
    mutationFn: () => api.post<Project>('/projects/' + projectId + '/reset_project_key'),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['projects'] }),
  });

  if (!p) return <div className="skel h-40 w-full rounded-3xl" />;

  return (
    <div className="flex flex-col gap-5">
      <section className="glass glass-edge p-6">
        <div className="text-[11px] uppercase tracking-[0.16em] text-ink-4 font-semibold mb-1">
          STEP 1
        </div>
        <h2 className="text-xl font-bold tracking-tight">
          {t('setup.step1.title')} <span className="grad-text">project_key</span>
        </h2>
        <p className="text-sm text-ink-3 mt-1 mb-5">
          {t('setup.step1.hint')}
        </p>
        <SecretField value={p.project_key} />
        <div className="mt-3 flex flex-wrap items-center gap-2">
          {!confirmReset ? (
            <button
              onClick={() => setConfirmReset(true)}
              className="inline-flex items-center gap-2 px-4 py-1.5 rounded-full
                         bg-amber-50 text-amber-800 border border-amber-200 hover:bg-amber-100
                         text-xs font-semibold transition"
            >
              {t('setup.step1.reset')}
            </button>
          ) : (
            <>
              <button onClick={() => setConfirmReset(false)} className="btn-ghost !text-[12px] !py-1.5 !px-3">
                {t('setup.step1.cancel')}
              </button>
              <button
                onClick={() => resetKey.mutate(undefined, { onSettled: () => setConfirmReset(false) })}
                disabled={resetKey.isPending}
                className="inline-flex items-center gap-2 px-4 py-1.5 rounded-full
                           bg-amber-500 hover:bg-amber-600 text-white text-xs font-semibold
                           shadow-[0_6px_18px_-6px_rgba(245,158,11,0.55)]
                           disabled:opacity-60"
              >
                {resetKey.isPending ? t('setup.step1.resetting') : t('setup.step1.confirm')}
              </button>
              <span className="text-[11px] text-ink-4">{t('setup.step1.resetNotice')}</span>
            </>
          )}
          {resetKey.isSuccess && (
            <span className="text-[11px] text-emerald-600">{t('setup.step1.resetDone')}</span>
          )}
          {resetKey.isError && (
            <span className="text-[11px] text-rose-600">{(resetKey.error as Error).message}</span>
          )}
        </div>
      </section>

      <section className="glass glass-edge p-6">
        <div className="text-[11px] uppercase tracking-[0.16em] text-ink-4 font-semibold mb-1">
          STEP 2
        </div>
        <h2 className="text-xl font-bold tracking-tight">{t('setup.step2.title')}</h2>
        <p className="text-sm text-ink-3 mt-1 mb-5">
          {t('setup.step2.hint')}
        </p>
        <div className="flex flex-col gap-2.5">
          {presets.map(({ label, key, grad }) => (
            <div key={label} className="flex items-center gap-3">
              <span className={`shrink-0 inline-flex items-center justify-center w-[96px]
                                rounded-full text-[11px] font-semibold text-white py-1
                                bg-gradient-to-r ${grad}
                                shadow-[0_2px_6px_-2px_rgba(15,23,42,0.2)]`}>
                {label}
              </span>
              <div className="flex-1 min-w-0 code-line truncate">{p.example[key]}</div>
              <button
                onClick={() => copy(label, p.example[key])}
                className="btn-ghost shrink-0 !text-[12px] !py-1.5 !px-3"
              >
                {copied === label ? '✓' : t('common.copy')}
              </button>
            </div>
          ))}
        </div>
      </section>

      <section className="glass glass-edge p-6">
        <div className="text-[11px] uppercase tracking-[0.16em] text-ink-4 font-semibold mb-1">
          STEP 3
        </div>
        <h2 className="text-xl font-bold tracking-tight">{t('setup.step3.title')}</h2>
        <p className="text-sm text-ink-3 mt-1 mb-5">
          {t('setup.step3.hint')}
        </p>
        <div className="grid lg:grid-cols-2 gap-3">
          <SnippetCard
            title="OpenAI · Python"
            code={`from openai import OpenAI

client = OpenAI(
    api_key="sk-real-openai-key",
    base_url="${p.example.openai}",
    default_headers={"X-AILens-Project-Key": "${p.project_key}"},
)
resp = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "hi"}],
)`}
          />
          <SnippetCard
            title="OpenAI · Node"
            code={`import OpenAI from 'openai';

const client = new OpenAI({
  apiKey: 'sk-real-openai-key',
  baseURL: '${p.example.openai}',
  defaultHeaders: { 'X-AILens-Project-Key': '${p.project_key}' },
});

await client.chat.completions.create({
  model: 'gpt-4o-mini',
  messages: [{ role: 'user', content: 'hi' }],
});`}
          />
          <SnippetCard
            title="Anthropic · Python"
            code={`import anthropic

client = anthropic.Anthropic(
    api_key="sk-ant-real-key",
    base_url="${p.example.anthropic}",
    default_headers={"X-AILens-Project-Key": "${p.project_key}"},
)
msg = client.messages.create(
    model="claude-3-5-sonnet-latest",
    max_tokens=256,
    messages=[{"role": "user", "content": "hi"}],
)`}
          />
          <SnippetCard
            title="Go · cloudwego/eino"
            code={`type kHeader struct{ key string; base http.RoundTripper }
func (t *kHeader) RoundTrip(r *http.Request) (*http.Response, error) {
    r = r.Clone(r.Context())
    r.Header.Set("X-AILens-Project-Key", t.key)
    b := t.base; if b == nil { b = http.DefaultTransport }
    return b.RoundTrip(r)
}

cm, _ := openai.NewChatModel(ctx, &openai.ChatModelConfig{
    APIKey:     "sk-real-key",
    Model:      "gpt-4o-mini",
    BaseURL:    "${p.example.openai}",
    HTTPClient: &http.Client{Transport: &kHeader{key: "${p.project_key}"}},
})`}
          />
        </div>
      </section>

      <section className="glass glass-edge p-6">
        <div className="text-[11px] uppercase tracking-[0.16em] text-ink-4 font-semibold mb-1">
          {t('setup.optional.label')}
        </div>
        <h2 className="text-xl font-bold tracking-tight">{t('setup.optional.title')}</h2>
        <p className="text-sm text-ink-3 mt-1 mb-4">
          {t('setup.optional.hint')}
        </p>
        <div className="grid sm:grid-cols-2 gap-2">
          {[
            ['X-AILens-User',       t('setup.meta.user.desc')],
            ['X-AILens-Session',    t('setup.meta.session.desc')],
            ['X-AILens-Trace-Id',   t('setup.meta.trace.desc')],
            ['X-AILens-Trace-Name', t('setup.meta.traceName.desc')],
          ].map(([k, v]) => (
            <div key={k} className="rounded-2xl bg-white/55 border border-white/70 px-3.5 py-3">
              <div className="mono text-[12.5px] font-semibold text-ink">{k}</div>
              <div className="text-[12px] text-ink-3 mt-1">{v}</div>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}

function SnippetCard({ title, code }: { title: string; code: string }) {
  const t = useT();
  const [done, setDone] = useState(false);
  return (
    <div className="codeblock-frame">
      <div className="codeblock-header">
        <span>{title}</span>
        <button
          onClick={() => { copyToClipboard(code); setDone(true); setTimeout(() => setDone(false), 1200); }}
          className="normal-case tracking-normal text-[11px] font-semibold text-slate-300 hover:text-white px-2 py-0.5 rounded-md
                     bg-slate-800/70 hover:bg-slate-700/80 transition"
        >
          {done ? t('common.copied') : t('common.copy')}
        </button>
      </div>
      <pre>{code}</pre>
    </div>
  );
}
