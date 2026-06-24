import type { Tiktoken, TiktokenEncoding } from 'js-tiktoken/lite';

export interface TokenizableContentPart {
  type?: string;
  text?: string;
  image_url?: { url?: string } | string;
  source?: { type?: string; media_type?: string; data?: string };
  id?: string;
  name?: string;
  input?: unknown;
  tool_use_id?: string;
  content?: string | TokenizableContentPart[];
  cache_control?: unknown;
}

export interface TokenizableMessage {
  role?: string;
  content?: string | TokenizableContentPart[];
  reasoning_content?: string;
  tool_calls?: unknown;
  tool_call_id?: string;
  name?: string;
  function_call?: unknown;
}

export interface TokenizableTool {
  raw: unknown;
}

export interface RequestTokenSegments {
  toolTokens: number[];
  messageTokens: number[];
}

const DEEPSEEK_BOS = '<｜begin▁of▁sentence｜>';
const DEEPSEEK_EOS = '<｜end▁of▁sentence｜>';
const DEEPSEEK_USER = '<｜User｜>';
const DEEPSEEK_ASSISTANT = '<｜Assistant｜>';
const DEEPSEEK_TOOL_OUTPUTS_BEGIN = '<｜tool▁outputs▁begin｜>';
const DEEPSEEK_TOOL_OUTPUT_BEGIN = '<｜tool▁output▁begin｜>';
const DEEPSEEK_TOOL_OUTPUT_END = '<｜tool▁output▁end｜>';
const DEEPSEEK_TOOL_OUTPUTS_END = '<｜tool▁outputs▁end｜>';

interface DeepSeekTokenizer {
  encode(text: string, options?: { add_special_tokens?: boolean }): { ids: number[] };
}

let deepSeekTokenizerPromise: Promise<DeepSeekTokenizer> | null = null;
const tiktokenCache = new Map<TiktokenEncoding, Promise<Tiktoken>>();

export async function estimateRequestTokenSegments(
  model: string,
  tools: TokenizableTool[],
  messages: TokenizableMessage[],
): Promise<RequestTokenSegments> {
  try {
    if (isDeepSeekModel(model)) {
      return await estimateDeepSeekSegments(tools, messages);
    }
    const openAIEncoding = encodingForOpenAIModel(model);
    if (openAIEncoding) {
      return await estimateOpenAISegments(openAIEncoding, tools, messages);
    }
  } catch {
    // Tokenizer assets are best-effort for UI annotation; never block body rendering.
  }
  return estimateHeuristicSegments(tools, messages);
}

function isDeepSeekModel(model: string): boolean {
  return model.toLowerCase().includes('deepseek');
}

function encodingForOpenAIModel(model: string): TiktokenEncoding | null {
  const m = model.toLowerCase();
  if (!m) return null;
  if (
    m.startsWith('gpt-5') ||
    m.startsWith('gpt-4o') ||
    m.startsWith('gpt-4.1') ||
    m.startsWith('gpt-4.5') ||
    m.startsWith('o1') ||
    m.startsWith('o3') ||
    m.startsWith('o4')
  ) {
    return 'o200k_base';
  }
  if (
    m.startsWith('gpt-4') ||
    m.startsWith('gpt-3.5') ||
    m.startsWith('gpt-35') ||
    m.startsWith('text-embedding-3') ||
    m.startsWith('text-embedding-ada-002')
  ) {
    return 'cl100k_base';
  }
  return null;
}

async function estimateDeepSeekSegments(
  tools: TokenizableTool[],
  messages: TokenizableMessage[],
): Promise<RequestTokenSegments> {
  const tokenizer = await loadDeepSeekTokenizer();
  const count = (text: string) => tokenizer.encode(text, { add_special_tokens: false }).ids.length;

  let seenSystem = false;
  let inToolOutput = false;

  return {
    toolTokens: tools.map((tool) => count(stableStringify(tool.raw))),
    messageTokens: messages.map((message) => {
      const role = (message.role || '').toLowerCase();
      const content = contentToText(message.content);

      if (role === 'system') {
        const prefix = seenSystem ? '\n\n' : DEEPSEEK_BOS;
        seenSystem = true;
        return count(prefix + content);
      }

      if (role === 'user') {
        inToolOutput = false;
        return count(DEEPSEEK_USER + content);
      }

      if (role === 'assistant') {
        if (inToolOutput) {
          inToolOutput = false;
          return count(DEEPSEEK_TOOL_OUTPUTS_END + stripThink(content) + DEEPSEEK_EOS);
        }
        return count(DEEPSEEK_ASSISTANT + stripThink(content) + deepSeekToolCallsText(message) + DEEPSEEK_EOS);
      }

      if (role === 'tool') {
        const prefix = inToolOutput ? DEEPSEEK_TOOL_OUTPUT_BEGIN : DEEPSEEK_TOOL_OUTPUTS_BEGIN + DEEPSEEK_TOOL_OUTPUT_BEGIN;
        inToolOutput = true;
        return count(prefix + content + DEEPSEEK_TOOL_OUTPUT_END);
      }

      return count(stableStringify(message));
    }),
  };
}

async function estimateOpenAISegments(
  encodingName: TiktokenEncoding,
  tools: TokenizableTool[],
  messages: TokenizableMessage[],
): Promise<RequestTokenSegments> {
  const encoder = await getCachedTiktoken(encodingName);
  const count = (text: string) => encoder.encode(text, 'all').length;

  return {
    toolTokens: tools.map((tool) => count(stableStringify(tool.raw))),
    messageTokens: messages.map((message) => estimateOpenAIMessageTokens(message, count)),
  };
}

function estimateOpenAIMessageTokens(
  message: TokenizableMessage,
  count: (text: string) => number,
): number {
  let total = 3; // ChatML-style per-message framing used by recent OpenAI chat models.
  total += count(message.role || '');
  if (message.name) total += 1 + count(message.name);
  if (message.tool_call_id) total += count(message.tool_call_id);
  total += count(contentToText(message.content));
  if (message.reasoning_content) total += count(message.reasoning_content);
  if (message.tool_calls) total += count(stableStringify(message.tool_calls));
  if (message.function_call) total += count(stableStringify(message.function_call));
  return total;
}

function estimateHeuristicSegments(
  tools: TokenizableTool[],
  messages: TokenizableMessage[],
): RequestTokenSegments {
  return {
    toolTokens: tools.map((tool) => heuristicCount(stableStringify(tool.raw))),
    messageTokens: messages.map((message) => heuristicCount(stableStringify(message))),
  };
}

function loadDeepSeekTokenizer(): Promise<DeepSeekTokenizer> {
  if (!deepSeekTokenizerPromise) {
    deepSeekTokenizerPromise = Promise.all([
      import('@huggingface/tokenizers'),
      fetch('/tokenizers/deepseek-v3/tokenizer.json').then((r) => {
        if (!r.ok) throw new Error(`DeepSeek tokenizer load failed: HTTP ${r.status}`);
        return r.json();
      }),
      fetch('/tokenizers/deepseek-v3/tokenizer_config.json').then((r) => {
        if (!r.ok) throw new Error(`DeepSeek tokenizer config load failed: HTTP ${r.status}`);
        return r.json();
      }),
    ]).then(([mod, tokenizerJSON, tokenizerConfig]) => new mod.Tokenizer(tokenizerJSON, tokenizerConfig));
  }
  return deepSeekTokenizerPromise;
}

function getCachedTiktoken(encodingName: TiktokenEncoding): Promise<Tiktoken> {
  const cached = tiktokenCache.get(encodingName);
  if (cached) return cached;
  const encoder = loadTiktoken(encodingName);
  tiktokenCache.set(encodingName, encoder);
  return encoder;
}

async function loadTiktoken(encodingName: TiktokenEncoding): Promise<Tiktoken> {
  const [{ Tiktoken }, ranks] = await Promise.all([
    import('js-tiktoken/lite'),
    importTiktokenRanks(encodingName),
  ]);
  return new Tiktoken(ranks.default);
}

function importTiktokenRanks(encodingName: TiktokenEncoding) {
  switch (encodingName) {
    case 'o200k_base':
      return import('js-tiktoken/ranks/o200k_base');
    case 'cl100k_base':
      return import('js-tiktoken/ranks/cl100k_base');
    default:
      return import('js-tiktoken/ranks/cl100k_base');
  }
}

function contentToText(content: TokenizableMessage['content']): string {
  if (content == null) return '';
  if (typeof content === 'string') return content;
  return content.map(contentPartToText).join('');
}

function contentPartToText(part: TokenizableContentPart): string {
  const type = (part.type || '').toLowerCase();
  if (type === 'text' && typeof part.text === 'string') return part.text;
  if (typeof part.content === 'string') return part.content;
  if (Array.isArray(part.content)) return part.content.map(contentPartToText).join('');
  return stableStringify(part);
}

function deepSeekToolCallsText(message: TokenizableMessage): string {
  if (!Array.isArray(message.tool_calls) || message.tool_calls.length === 0) return '';
  return stableStringify(message.tool_calls);
}

function stripThink(content: string): string {
  const marker = '</think>';
  const idx = content.lastIndexOf(marker);
  return idx >= 0 ? content.slice(idx + marker.length) : content;
}

function heuristicCount(text: string): number {
  if (!text.trim()) return 0;

  let cjk = 0;
  let other = 0;
  let whitespace = 0;

  for (const ch of text) {
    if (/\s/.test(ch)) {
      whitespace++;
    } else if (/[\u4e00-\u9fff\u3040-\u30ff\uac00-\ud7af]/.test(ch)) {
      cjk++;
    } else {
      other++;
    }
  }

  return cjk + Math.ceil((other + whitespace) / 3.5);
}

function stableStringify(value: unknown): string {
  if (value === undefined) return '';
  if (value === null) return 'null';
  if (typeof value === 'string') return JSON.stringify(value);
  if (typeof value !== 'object') return String(value);
  if (Array.isArray(value)) return '[' + value.map(stableStringify).join(',') + ']';
  const obj = value as Record<string, unknown>;
  return '{' + Object.keys(obj).sort()
    .filter((key) => obj[key] !== undefined)
    .map((key) => JSON.stringify(key) + ':' + stableStringify(obj[key]))
    .join(',') + '}';
}
