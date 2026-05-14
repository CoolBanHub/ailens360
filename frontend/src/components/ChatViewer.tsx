// ChatViewer renders an OpenAI / Anthropic-style messages payload as a
// chat-like transcript: role pills, text bubbles, tool call cards, and inline
// images for content parts of type image_url. Falls back to JSON when the
// shape doesn't look like a chat message list.
//
// Usage: <ChatViewer raw={t.RequestBody} mode="request" /> or mode="response".

import { useState } from 'react';
import { prettyJSON } from '../lib/fmt';
import { useT } from '../i18n';

type Role = 'system' | 'user' | 'assistant' | 'tool' | 'developer' | 'function';

interface ToolCall {
  id?: string;
  type?: string;
  function?: { name?: string; arguments?: string };
  // Anthropic style
  name?: string;
  input?: unknown;
}

interface ContentPart {
  type?: string;
  text?: string;
  image_url?: { url?: string } | string;
  source?: { type?: string; media_type?: string; data?: string }; // anthropic image
  // Anthropic tool_use
  id?: string;
  name?: string;
  input?: unknown;
  // Anthropic tool_result
  tool_use_id?: string;
  content?: string | ContentPart[];
}

interface Message {
  role?: Role | string;
  content?: string | ContentPart[];
  tool_calls?: ToolCall[];
  tool_call_id?: string;
  name?: string;
  function_call?: { name?: string; arguments?: string };
}

interface ChatPayload {
  messages?: Message[];
  // anthropic responses
  content?: ContentPart[] | string;
  role?: Role | string;
  // openai chat completion response
  choices?: Array<{ message?: Message; finish_reason?: string }>;
  // misc
  model?: string;
  tools?: unknown;
  usage?: unknown;
}

interface Props {
  raw: string | null | undefined;
  mode: 'request' | 'response';
}

export default function ChatViewer({ raw, mode }: Props) {
  const t = useT();
  const [view, setView] = useState<'pretty' | 'raw'>('pretty');

  // Streaming responses arrive as raw SSE (`data: {...}\n\n` frames) in `raw`.
  // Reassemble them client-side so the pretty view can render the same chat
  // bubble shape it uses for non-stream completions, while raw view still
  // shows the original SSE bytes verbatim for debugging.
  const messages = mode === 'response' && looksLikeOpenAISSE(raw)
    ? assembleOpenAIStream(raw)
    : extractMessages(parseJSON(raw), mode);
  // Render Pretty only when we found something useful; otherwise default to Raw
  const canPretty = messages.length > 0;
  const effective = canPretty ? view : 'raw';

  return (
    <div className="flex flex-col gap-2">
      {canPretty && (
        <div className="flex justify-end">
          <div
            className="relative inline-flex items-center rounded-full
                       bg-white/55 ring-1 ring-[color:var(--glass-line)]
                       backdrop-blur-sm shadow-[0_1px_2px_rgba(15,23,42,0.04)]"
            role="tablist"
            aria-label="view mode"
          >
            {/* Brand-gradient indicator */}
            <span
              aria-hidden
              className="absolute inset-y-0 left-0 w-1/2 rounded-full
                         bg-gradient-to-r from-[var(--grad-1)] via-[var(--grad-2)] to-[var(--grad-3)]
                         shadow-[0_4px_14px_-4px_rgba(99,102,241,0.55)]
                         transition-transform duration-300 ease-[cubic-bezier(.4,0,.2,1)]"
              style={{ transform: view === 'pretty' ? 'translateX(0)' : 'translateX(100%)' }}
            />
            {(['pretty', 'raw'] as const).map((m) => {
              const active = view === m;
              return (
                <button
                  key={m}
                  role="tab"
                  aria-selected={active}
                  onClick={() => setView(m)}
                  className={
                    'relative z-10 px-4 py-1.5 rounded-full text-[11.5px] font-semibold ' +
                    'tracking-[0.04em] transition-colors duration-200 ' +
                    (active ? 'text-white' : 'text-ink-4 hover:text-ink-2')
                  }
                >
                  {t(m === 'pretty' ? 'detail.viewMode.pretty' : 'detail.viewMode.raw')}
                </button>
              );
            })}
          </div>
        </div>
      )}

      {effective === 'pretty' ? (
        <div className="flex flex-col gap-2">
          {messages.map((m, i) => <MessageBubble key={i} m={m} />)}
        </div>
      ) : (
        <pre className="codeblock">{prettyJSON(raw)}</pre>
      )}
    </div>
  );
}

/* ── parsing helpers ────────────────────────────────────────── */

function parseJSON(s: string | null | undefined): ChatPayload | null {
  if (!s) return null;
  try { return JSON.parse(s) as ChatPayload; }
  catch { return null; }
}

/* ── SSE assembly (streaming response → pretty message) ────── */

// Treat the body as OpenAI-style SSE when at least one frame line begins
// with `data: {`. Anthropic's typed events (`event: ...\ndata: {...}`) also
// match — we'd need a different assembler for them, so be strict: only
// accept frames whose payload looks like an OpenAI chunk (has `choices`).
function looksLikeOpenAISSE(s: string | null | undefined): boolean {
  if (!s) return false;
  // Cheap pre-check before full parse.
  if (!/^data:\s*\{/m.test(s)) return false;
  for (const line of s.split('\n')) {
    if (!line.startsWith('data:')) continue;
    const payload = line.slice(5).trim();
    if (!payload || payload === '[DONE]') continue;
    try {
      const obj = JSON.parse(payload);
      if (obj && Array.isArray(obj.choices)) return true;
    } catch { /* malformed chunk — keep scanning */ }
    return false;
  }
  return false;
}

interface OpenAIToolDelta {
  index?: number;
  id?: string;
  type?: string;
  function?: { name?: string; arguments?: string };
}

interface OpenAIChunk {
  choices?: Array<{
    index?: number;
    delta?: {
      role?: string;
      content?: string;
      tool_calls?: OpenAIToolDelta[];
    };
    finish_reason?: string | null;
  }>;
}

interface ToolCallBuilder {
  id?: string;
  type?: string;
  name?: string;
  args: string;
}

interface ChoiceBuilder {
  role: string;
  content: string;
  toolBuilders: Map<number, ToolCallBuilder>;
  toolOrder: number[];
}

// assembleOpenAIStream folds OpenAI-style SSE frames back into one Message
// per `choices[*].index`. When the request used n>1, each index carries an
// independent completion (possibly with its own tool_calls); merging them
// into a single message would mix two different model outputs together.
// Within a single choice, `tool_calls[*].index` accumulates pieces of one
// tool call (`function.arguments` is streamed character-by-character).
function assembleOpenAIStream(raw: string | null | undefined): Message[] {
  if (!raw) return [];
  const choices = new Map<number, ChoiceBuilder>();
  const choiceOrder: number[] = [];

  const choiceFor = (idx: number): ChoiceBuilder => {
    let cb = choices.get(idx);
    if (!cb) {
      cb = { role: 'assistant', content: '', toolBuilders: new Map(), toolOrder: [] };
      choices.set(idx, cb);
      choiceOrder.push(idx);
    }
    return cb;
  };

  for (const line of raw.split('\n')) {
    if (!line.startsWith('data:')) continue;
    const payload = line.slice(5).trim();
    if (!payload || payload === '[DONE]') continue;
    let chunk: OpenAIChunk;
    try { chunk = JSON.parse(payload) as OpenAIChunk; } catch { continue; }
    if (!Array.isArray(chunk.choices)) continue;
    for (const choice of chunk.choices) {
      const delta = choice.delta;
      if (!delta) continue;
      const cb = choiceFor(choice.index ?? 0);
      if (delta.role) cb.role = delta.role;
      if (typeof delta.content === 'string') cb.content += delta.content;
      if (Array.isArray(delta.tool_calls)) {
        for (const tcd of delta.tool_calls) {
          const tIdx = tcd.index ?? 0;
          let tb = cb.toolBuilders.get(tIdx);
          if (!tb) { tb = { args: '' }; cb.toolBuilders.set(tIdx, tb); cb.toolOrder.push(tIdx); }
          if (tcd.id) tb.id = tcd.id;
          if (tcd.type) tb.type = tcd.type;
          if (tcd.function?.name) tb.name = tcd.function.name;
          if (tcd.function?.arguments) tb.args += tcd.function.arguments;
        }
      }
    }
  }

  // Render in choice-index order (0,1,2,...) — same shape as the non-stream
  // response which already emits one Message per choice.
  choiceOrder.sort((a, b) => a - b);

  const out: Message[] = [];
  for (const idx of choiceOrder) {
    const cb = choices.get(idx)!;
    if (!cb.content && cb.toolOrder.length === 0) continue;
    const msg: Message = { role: cb.role as Role, content: cb.content };
    if (cb.toolOrder.length > 0) {
      msg.tool_calls = cb.toolOrder.map((tIdx) => {
        const tb = cb.toolBuilders.get(tIdx)!;
        return {
          id: tb.id,
          type: tb.type || 'function',
          function: { name: tb.name || '', arguments: tb.args },
        };
      });
    }
    out.push(msg);
  }
  return out;
}

/**
 * Pull a flat list of "messages" out of either:
 *   - Request: OpenAI chat completions `{ messages: [...] }` or Anthropic Messages API request.
 *   - Response: OpenAI `{ choices: [{ message: {...} }] }` or Anthropic `{ role, content: [...] }`.
 */
function extractMessages(p: ChatPayload | null, mode: 'request' | 'response'): Message[] {
  if (!p) return [];
  if (mode === 'request') {
    if (Array.isArray(p.messages)) return p.messages;
    return [];
  }
  // response
  if (Array.isArray(p.choices)) {
    return p.choices
      .map((c) => c?.message)
      .filter((m): m is Message => !!m && (typeof m === 'object'));
  }
  // Anthropic-style response: top-level role + content
  if (p.role && (typeof p.content === 'string' || Array.isArray(p.content))) {
    return [{ role: p.role, content: p.content }];
  }
  return [];
}

const ROLE_TINT: Record<string, string> = {
  system:    'bg-amber-50/70  border-amber-200/60  text-amber-900',
  developer: 'bg-amber-50/70  border-amber-200/60  text-amber-900',
  user:      'bg-sky-50/70    border-sky-200/60    text-sky-900',
  assistant: 'bg-emerald-50/70 border-emerald-200/60 text-emerald-900',
  tool:      'bg-violet-50/70 border-violet-200/60 text-violet-900',
  function:  'bg-violet-50/70 border-violet-200/60 text-violet-900',
};

function MessageBubble({ m }: { m: Message }) {
  const role = (m.role || 'user').toLowerCase();
  const tint = ROLE_TINT[role] || 'bg-white/55 border-white/70 text-ink';

  return (
    <div className={'rounded-2xl border px-3.5 py-2.5 ' + tint}>
      <div className="flex items-center gap-2 mb-1.5">
        <span className="text-[10px] uppercase tracking-[0.16em] font-bold">{role}</span>
        {m.name && (
          <span className="mono text-[10.5px] opacity-70">{m.name}</span>
        )}
        {m.tool_call_id && (
          <span className="mono text-[10.5px] opacity-70">→ {m.tool_call_id}</span>
        )}
      </div>

      <ContentRenderer content={m.content} />

      {/* OpenAI: tool_calls on assistant messages */}
      {Array.isArray(m.tool_calls) && m.tool_calls.length > 0 && (
        <div className="mt-2 flex flex-col gap-1.5">
          {m.tool_calls.map((tc, i) => (
            <ToolCallCard key={i} tc={tc} />
          ))}
        </div>
      )}

      {/* OpenAI legacy: function_call */}
      {m.function_call && (
        <div className="mt-2">
          <ToolCallCard tc={{ function: m.function_call }} />
        </div>
      )}
    </div>
  );
}

function ContentRenderer({ content }: { content: Message['content'] }) {
  if (content == null) return null;

  if (typeof content === 'string') {
    if (!content.trim()) return <span className="text-ink-4 italic text-[12.5px]">(empty)</span>;
    return <TextBlock>{content}</TextBlock>;
  }

  // array of parts (multimodal / anthropic blocks)
  return (
    <div className="flex flex-col gap-1.5">
      {content.map((part, i) => <ContentPartView key={i} part={part} />)}
    </div>
  );
}

function ContentPartView({ part }: { part: ContentPart }) {
  const type = (part.type || '').toLowerCase();

  // Text part (OpenAI: type=text; Anthropic: type=text)
  if (type === 'text' && typeof part.text === 'string') {
    return <TextBlock>{part.text}</TextBlock>;
  }

  // OpenAI image_url part
  if (type === 'image_url' && part.image_url) {
    const url = typeof part.image_url === 'string' ? part.image_url : part.image_url.url || '';
    return <ImagePart url={url} />;
  }

  // Anthropic image part (base64 in source)
  if (type === 'image' && part.source?.data && part.source?.media_type) {
    const dataUri = 'data:' + part.source.media_type + ';base64,' + part.source.data;
    return <ImagePart url={dataUri} />;
  }

  // Anthropic tool_use block
  if (type === 'tool_use') {
    return (
      <ToolCallCard tc={{
        id: part.id,
        type: 'function',
        function: {
          name: part.name,
          arguments: typeof part.input === 'string' ? part.input : JSON.stringify(part.input ?? {}, null, 2),
        },
      }} />
    );
  }

  // Anthropic tool_result block
  if (type === 'tool_result') {
    const text = typeof part.content === 'string'
      ? part.content
      : Array.isArray(part.content)
        ? part.content.map((p) => p.text || '').join('\n')
        : '';
    return (
      <div className="rounded-xl bg-white/55 border border-white/70 px-3 py-2">
        <div className="text-[10px] uppercase tracking-[0.14em] text-ink-4 font-semibold mb-1">
          tool_result {part.tool_use_id && <span className="mono normal-case opacity-70">· {part.tool_use_id}</span>}
        </div>
        <TextBlock>{text}</TextBlock>
      </div>
    );
  }

  // Fallback for unknown part type — show its JSON
  return (
    <pre className="text-[11.5px] mono bg-white/55 border border-white/70 rounded-xl p-2 overflow-auto">
      {JSON.stringify(part, null, 2)}
    </pre>
  );
}

function TextBlock({ children }: { children: string }) {
  return (
    <div className="text-[13px] leading-relaxed whitespace-pre-wrap break-words">
      {children}
    </div>
  );
}

function ImagePart({ url }: { url: string }) {
  return (
    <div className="inline-block rounded-xl overflow-hidden border border-white/70 bg-white/40 max-w-[260px]">
      {/* eslint-disable-next-line jsx-a11y/img-redundant-alt */}
      <img src={url} alt="image content"
           className="block max-w-[260px] max-h-[260px] object-contain"
           onError={(e) => { (e.currentTarget as HTMLImageElement).style.display = 'none'; }} />
      <div className="text-[10px] mono text-ink-4 px-2 py-1 truncate">{url.slice(0, 80)}{url.length > 80 ? '…' : ''}</div>
    </div>
  );
}

function ToolCallCard({ tc }: { tc: ToolCall }) {
  const name = tc.function?.name || tc.name || '(tool)';
  const argsRaw = tc.function?.arguments ?? (typeof tc.input === 'string' ? tc.input : JSON.stringify(tc.input ?? {}, null, 2));
  let argsPretty = argsRaw;
  try { argsPretty = JSON.stringify(JSON.parse(argsRaw), null, 2); } catch { /* keep raw */ }

  return (
    <div className="rounded-xl bg-white/60 border border-white/75 overflow-hidden">
      <div className="flex items-center gap-2 px-3 py-1.5 border-b border-[color:var(--glass-line)] bg-white/35">
        <span className="inline-flex items-center justify-center w-5 h-5 rounded-md
                         bg-gradient-to-br from-violet-400 to-indigo-400 text-white text-[10px] font-bold">
          ƒ
        </span>
        <span className="font-semibold text-[12.5px] mono">{name}</span>
        {tc.id && <span className="ml-auto mono text-[10.5px] text-ink-4 truncate max-w-[200px]">{tc.id}</span>}
      </div>
      <pre className="codeblock !rounded-none !border-0 !max-h-[280px]">{argsPretty}</pre>
    </div>
  );
}
