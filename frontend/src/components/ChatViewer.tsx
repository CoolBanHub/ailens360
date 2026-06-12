// ChatViewer renders an OpenAI / Anthropic-style messages payload as a
// chat-like transcript: role pills, text bubbles, tool call cards, and inline
// images for content parts of type image_url. Falls back to JSON when the
// shape doesn't look like a chat message list.
//
// Usage: <ChatViewer raw={t.RequestBody} mode="request" /> or mode="response".

import { useState } from 'react';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { copyToClipboard, prettyJSON } from '../lib/fmt';
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
  reasoning_content?: string;
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
  const parsed = parseJSON(raw);

  // Streaming responses arrive as raw SSE (`data: {...}\n\n` frames) in `raw`.
  // Reassemble them client-side so the pretty view can render the same chat
  // bubble shape it uses for non-stream completions, while raw view still
  // shows the original SSE bytes verbatim for debugging.
  const streamMessages = mode === 'response' ? assembleStream(raw) : [];
  const messages = streamMessages.length > 0 ? streamMessages : extractMessages(parsed, mode);
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
        <RawBlock raw={raw} />
      )}
    </div>
  );
}

// Raw view of the body with a copy button. Copies the ORIGINAL bytes (not
// the prettified display) so the user can paste straight into curl or another
// client to replay the call.
function RawBlock({ raw }: { raw: string | null | undefined }) {
  const t = useT();
  const [copied, setCopied] = useState(false);
  const canCopy = !!raw && raw.length > 0;
  const onCopy = () => {
    if (!canCopy) return;
    copyToClipboard(raw!);
    setCopied(true);
    setTimeout(() => setCopied(false), 1200);
  };
  return (
    <div className="relative">
      <pre className="codeblock">{prettyJSON(raw)}</pre>
      {canCopy && (
        <button
          type="button"
          onClick={onCopy}
          title={copied ? t('common.copied') : t('common.copy')}
          aria-label={t('common.copy')}
          className={
            'absolute top-2 right-2 inline-flex items-center gap-1 ' +
            'px-2 py-1 rounded-md text-[11px] font-medium ' +
            'bg-white/10 hover:bg-white/20 text-white/85 hover:text-white ' +
            'border border-white/15 backdrop-blur-sm transition'
          }
        >
          {copied ? (
            <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                 strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"
                 className="text-emerald-300">
              <path d="M5 13l4 4L19 7"/>
            </svg>
          ) : (
            <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                 strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <rect x="9" y="9" width="13" height="13" rx="2"/>
              <path d="M5 15V5a2 2 0 0 1 2-2h10"/>
            </svg>
          )}
          <span>{copied ? t('common.copied') : t('common.copy')}</span>
        </button>
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
      reasoning_content?: string;
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
  reasoningContent: string;
  toolBuilders: Map<number, ToolCallBuilder>;
  toolOrder: number[];
}

interface SSEFrame {
  event: string;
  data: string;
}

function assembleStream(raw: string | null | undefined): Message[] {
  const frames = parseSSE(raw);
  if (frames.length === 0) return [];

  const objects = frames
    .map((f) => ({ event: f.event, obj: parseJSONObject(f.data) }))
    .filter((x): x is { event: string; obj: Record<string, any> } => !!x.obj);

  if (objects.some((x) => typeof x.obj.type === 'string' && x.obj.type.startsWith('response.'))) {
    return assembleResponsesStream(objects);
  }
  if (frames.some((f) => f.event === 'message_start' || f.event === 'content_block_delta' || f.event === 'message_delta')) {
    return assembleAnthropicStream(objects);
  }
  if (objects.some((x) => Array.isArray(x.obj.choices))) {
    return assembleOpenAIStream(raw);
  }
  return [];
}

function parseSSE(raw: string | null | undefined): SSEFrame[] {
  if (!raw || !/^data:/m.test(raw)) return [];
  const out: SSEFrame[] = [];
  let event = '';
  let data: string[] = [];

  const flush = () => {
    if (data.length > 0) out.push({ event, data: data.join('\n') });
    event = '';
    data = [];
  };

  for (const line of raw.split(/\r?\n/)) {
    if (line === '') {
      flush();
      continue;
    }
    if (line.startsWith('event:')) {
      event = line.slice(6).trim();
    } else if (line.startsWith('data:')) {
      const v = line.slice(5).trim();
      if (v && v !== '[DONE]') data.push(v);
    }
  }
  flush();
  return out;
}

function parseJSONObject(s: string): Record<string, any> | null {
  try {
    const v = JSON.parse(s);
    return v && typeof v === 'object' && !Array.isArray(v) ? v : null;
  } catch {
    return null;
  }
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
      cb = { role: 'assistant', content: '', reasoningContent: '', toolBuilders: new Map(), toolOrder: [] };
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
      if (typeof delta.reasoning_content === 'string') cb.reasoningContent += delta.reasoning_content;
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
    if (!cb.content && !cb.reasoningContent && cb.toolOrder.length === 0) continue;
    const msg: Message = { role: cb.role as Role };
    if (cb.content) msg.content = cb.content;
    if (cb.reasoningContent) msg.reasoning_content = cb.reasoningContent;
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

function assembleResponsesStream(frames: Array<{ event: string; obj: Record<string, any> }>): Message[] {
  const msg: Message = { role: 'assistant', content: '' };
  const tools = new Map<string, ToolCallBuilder>();
  const order: string[] = [];

  const toolFor = (key: string): ToolCallBuilder => {
    let tb = tools.get(key);
    if (!tb) {
      tb = { args: '' };
      tools.set(key, tb);
      order.push(key);
    }
    return tb;
  };

  for (const { obj } of frames) {
    const typ = String(obj.type || '');
    if ((typ === 'response.output_text.delta' || typ === 'response.refusal.delta') && typeof obj.delta === 'string') {
      msg.content = String(msg.content || '') + obj.delta;
      continue;
    }
    if (typ === 'response.output_text.done' && !msg.content && typeof obj.text === 'string') {
      msg.content = obj.text;
      continue;
    }
    if (typ === 'response.function_call_arguments.delta') {
      const key = String(obj.item_id || obj.call_id || obj.output_index || order.length || '0');
      toolFor(key).args += typeof obj.delta === 'string' ? obj.delta : '';
      continue;
    }
    if (typ === 'response.function_call_arguments.done') {
      const key = String(obj.item_id || obj.call_id || obj.output_index || order.length || '0');
      if (typeof obj.arguments === 'string') toolFor(key).args = obj.arguments;
      continue;
    }
    if (typ === 'response.output_item.added' || typ === 'response.output_item.done') {
      captureResponsesItem(obj.item, toolFor, order.length);
      continue;
    }
    if ((typ === 'response.completed' || typ === 'response.incomplete') && obj.response) {
      captureResponsesObject(obj.response, msg, toolFor);
    }
  }

  if (order.length > 0) {
    msg.tool_calls = order.map((key) => {
      const tb = tools.get(key)!;
      return {
        id: tb.id,
        type: tb.type || 'function',
        function: { name: tb.name || '', arguments: tb.args },
      };
    });
  }
  return msg.content || msg.tool_calls?.length ? [msg] : [];
}

function captureResponsesItem(
  item: any,
  toolFor: (key: string) => ToolCallBuilder,
  fallbackIndex: number,
) {
  if (!item || typeof item !== 'object') return;
  if (item.type !== 'function_call') return;
  const key = String(item.id || item.call_id || fallbackIndex);
  const tb = toolFor(key);
  if (item.id || item.call_id) tb.id = item.id || item.call_id;
  tb.type = 'function';
  if (item.name) tb.name = String(item.name);
  if (typeof item.arguments === 'string') tb.args = item.arguments;
}

function captureResponsesObject(resp: any, msg: Message, toolFor: (key: string) => ToolCallBuilder) {
  if (!resp || typeof resp !== 'object') return;
  if (!msg.content && typeof resp.output_text === 'string') {
    msg.content = resp.output_text;
  }
  if (!Array.isArray(resp.output)) return;
  const text: string[] = [];
  for (const item of resp.output) {
    if (item?.type === 'message' && Array.isArray(item.content)) {
      for (const part of item.content) {
        if ((part?.type === 'output_text' || part?.type === 'text') && typeof part.text === 'string') {
          text.push(part.text);
        }
      }
    } else {
      captureResponsesItem(item, toolFor, 0);
    }
  }
  if (!msg.content && text.length > 0) msg.content = text.join('\n');
}

function assembleAnthropicStream(frames: Array<{ event: string; obj: Record<string, any> }>): Message[] {
  const msg: Message = { role: 'assistant', content: '' };
  const parts = new Map<number, ContentPart>();
  const order: number[] = [];
  let fallbackIndex = 0;

  const partFor = (idx: number): ContentPart => {
    let part = parts.get(idx);
    if (!part) {
      part = { type: 'text', text: '' };
      parts.set(idx, part);
      order.push(idx);
    }
    return part;
  };

  for (const { event, obj } of frames) {
    if (event === 'message_start' && obj.message?.role) {
      msg.role = obj.message.role;
      continue;
    }
    if (event === 'content_block_start') {
      const idx = typeof obj.index === 'number' ? obj.index : fallbackIndex++;
      const block = obj.content_block;
      if (block?.type === 'tool_use') {
        parts.set(idx, { type: 'tool_use', id: block.id, name: block.name, input: block.input ?? {} });
        order.push(idx);
      } else {
        partFor(idx);
      }
      continue;
    }
    if (event === 'content_block_delta') {
      const idx = typeof obj.index === 'number' ? obj.index : 0;
      const delta = obj.delta;
      if (delta?.type === 'text_delta' && typeof delta.text === 'string') {
        const part = partFor(idx);
        part.type = 'text';
        part.text = (part.text || '') + delta.text;
      } else if (delta?.type === 'input_json_delta' && typeof delta.partial_json === 'string') {
        const part = partFor(idx);
        part.type = 'tool_use';
        part.input = String(part.input || '') + delta.partial_json;
      }
    }
  }

  const content = order
    .map((idx) => parts.get(idx)!)
    .filter((part) => part.type !== 'text' || !!part.text);
  if (content.length === 0) return [];
  msg.content = content.length === 1 && content[0].type === 'text' ? content[0].text || '' : content;
  return [msg];
}

/**
 * Pull a flat list of "messages" out of either:
 *   - Request: OpenAI chat completions `{ messages: [...] }` or Anthropic Messages API request.
 *   - Response: OpenAI `{ choices: [{ message: {...} }] }` or Anthropic `{ role, content: [...] }`.
 */
function extractMessages(p: ChatPayload | null, mode: 'request' | 'response'): Message[] {
  if (!p) return [];
  if (mode === 'request') {
    const responsesReq = extractResponsesRequest(p);
    if (responsesReq.length > 0) return responsesReq;
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
  const responsesResp = extractResponsesResponse(p);
  if (responsesResp.length > 0) return responsesResp;
  return [];
}

function extractResponsesRequest(p: ChatPayload): Message[] {
  const out: Message[] = [];
  const any = p as any;
  if (typeof any.instructions === 'string' && any.instructions.trim()) {
    out.push({ role: 'developer', content: any.instructions });
  }
  if (typeof any.input === 'string') {
    out.push({ role: 'user', content: any.input });
  } else if (Array.isArray(any.input)) {
    for (const item of any.input) {
      const msg = normalizeResponsesMessage(item);
      if (msg) out.push(msg);
    }
  }
  return out;
}

function extractResponsesResponse(p: ChatPayload): Message[] {
  const any = p as any;
  const msg: Message = { role: 'assistant', content: '' };
  if (typeof any.output_text === 'string' && any.output_text) {
    msg.content = any.output_text;
  }
  if (Array.isArray(any.output)) {
    const content: ContentPart[] = [];
    const toolCalls: ToolCall[] = [];
    for (const item of any.output) {
      if (item?.type === 'message') {
        const m = normalizeResponsesMessage(item);
        if (m) {
          if (typeof m.content === 'string') content.push({ type: 'text', text: m.content });
          else if (Array.isArray(m.content)) content.push(...m.content);
        }
      } else if (item?.type === 'function_call') {
        toolCalls.push({
          id: item.id || item.call_id,
          type: 'function',
          function: { name: item.name || '', arguments: item.arguments || '' },
        });
      }
    }
    if (!msg.content && content.length > 0) {
      msg.content = content.length === 1 ? content[0].text || '' : content;
    }
    if (toolCalls.length > 0) msg.tool_calls = toolCalls;
  }
  return msg.content || msg.tool_calls?.length ? [msg] : [];
}

function normalizeResponsesMessage(item: any): Message | null {
  if (!item || typeof item !== 'object') return null;
  if (item.type !== 'message' && !item.role && !item.content) return null;
  const role = item.role || (item.type === 'message' ? 'assistant' : 'user');
  const content = normalizeResponsesContent(item.content);
  return content == null ? null : { role, content };
}

function normalizeResponsesContent(content: any): Message['content'] | null {
  if (typeof content === 'string') return content;
  if (!Array.isArray(content)) return null;
  const parts: ContentPart[] = [];
  for (const part of content) {
    if (typeof part === 'string') {
      parts.push({ type: 'text', text: part });
      continue;
    }
    if (!part || typeof part !== 'object') continue;
    if ((part.type === 'input_text' || part.type === 'output_text' || part.type === 'text') && typeof part.text === 'string') {
      parts.push({ type: 'text', text: part.text });
    } else if (part.type === 'input_image' && part.image_url) {
      parts.push({ type: 'image_url', image_url: part.image_url });
    } else {
      parts.push(part as ContentPart);
    }
  }
  if (parts.length === 0) return null;
  if (parts.length === 1 && parts[0].type === 'text') return parts[0].text || '';
  return parts;
}

const ROLE_TINT: Record<string, string> = {
  system:    'bg-amber-50/70  border-amber-200/60  text-amber-900',
  developer: 'bg-amber-50/70  border-amber-200/60  text-amber-900',
  user:      'bg-blue-100/80    border-blue-300/70    text-blue-950',
  assistant: 'bg-fuchsia-50/80  border-fuchsia-300/60 text-fuchsia-950',
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

      <ContentRenderer content={m.content} role={role} />

      {typeof m.reasoning_content === 'string' && m.reasoning_content.trim() && (
        <div className="mt-2 rounded-xl bg-white/40 border border-dashed border-white/70 px-3 py-2">
          <div className="text-[10px] uppercase tracking-[0.14em] text-ink-4 font-semibold mb-1">
            reasoning
          </div>
          <TextBlock>{m.reasoning_content}</TextBlock>
        </div>
      )}

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

function ContentRenderer({ content, role }: { content: Message['content']; role: string }) {
  if (content == null) return null;

  if (typeof content === 'string') {
    if (!content.trim()) return <span className="text-ink-4 italic text-[12.5px]">(empty)</span>;
    return <TextBlock role={role}>{content}</TextBlock>;
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

// Threshold above which TextBlock starts collapsed. Picked to keep a long
// system prompt readable at a glance (first ~8 lines or ~600 chars) without
// requiring a click to scan a short user message.
const COLLAPSE_MIN_CHARS = 600;
const COLLAPSE_MIN_LINES = 8;
const COLLAPSE_PREVIEW_CHARS = 600;
const COLLAPSE_PREVIEW_LINES = 8;

const MARKDOWN_ROLES = new Set(['user', 'assistant']);

function TextBlock({ children, role }: { children: string; role?: string }) {
  const t = useT();
  const lineCount = children.split('\n').length;
  const needsCollapse = children.length > COLLAPSE_MIN_CHARS || lineCount > COLLAPSE_MIN_LINES;
  const [expanded, setExpanded] = useState(false);
  const useMarkdown = !!role && MARKDOWN_ROLES.has(role);

  const renderText = (text: string) => {
    if (useMarkdown) {
      return (
        <Markdown
          remarkPlugins={[remarkGfm]}
          components={{
            pre: ({ children }) => (
              <pre className="codeblock !rounded-lg !max-h-[320px] my-1.5">{children}</pre>
            ),
            code: ({ children, className, ...props }) => {
              const isBlock = className?.includes('language-');
              return isBlock ? (
                <code className={className} {...props}>{children}</code>
              ) : (
                <code className="px-1 py-0.5 rounded bg-black/5 text-[12.5px] font-mono" {...props}>{children}</code>
              );
            },
            p: ({ children }) => <p className="mb-1.5 last:mb-0">{children}</p>,
            ul: ({ children }) => <ul className="list-disc pl-5 mb-1.5 space-y-0.5">{children}</ul>,
            ol: ({ children }) => <ol className="list-decimal pl-5 mb-1.5 space-y-0.5">{children}</ol>,
            blockquote: ({ children }) => (
              <blockquote className="border-l-3 border-current/20 pl-3 my-1.5 opacity-85">{children}</blockquote>
            ),
            h1: ({ children }) => <h1 className="text-lg font-bold mb-1.5">{children}</h1>,
            h2: ({ children }) => <h2 className="text-base font-bold mb-1">{children}</h2>,
            h3: ({ children }) => <h3 className="text-sm font-bold mb-1">{children}</h3>,
            table: ({ children }) => (
              <div className="overflow-x-auto my-1.5">
                <table className="text-[12px] border-collapse border border-black/10">{children}</table>
              </div>
            ),
            th: ({ children }) => (
              <th className="border border-black/10 px-2 py-1 bg-black/5 font-semibold text-left">{children}</th>
            ),
            td: ({ children }) => (
              <td className="border border-black/10 px-2 py-1">{children}</td>
            ),
          }}
        >
          {text}
        </Markdown>
      );
    }
    return <>{text}</>;
  };

  if (!needsCollapse) {
    return (
      <div className={'text-[13px] leading-relaxed break-words' + (useMarkdown ? '' : ' whitespace-pre-wrap')}>
        {renderText(children)}
      </div>
    );
  }

  const lines = children.split('\n');
  const byLines = lines.slice(0, COLLAPSE_PREVIEW_LINES).join('\n');
  const byChars = children.slice(0, COLLAPSE_PREVIEW_CHARS);
  const preview = byLines.length <= byChars.length ? byLines : byChars;
  const remaining = children.length - preview.length;

  return (
    <div className={'text-[13px] leading-relaxed break-words' + (useMarkdown ? '' : ' whitespace-pre-wrap')}>
      {renderText(expanded ? children : preview)}
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className="mt-1.5 block text-[12px] font-medium text-indigo-600 hover:text-indigo-700 hover:underline"
      >
        {expanded ? t('detail.text.collapse') : t('detail.text.expand', { count: remaining })}
      </button>
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
