// Shapes returned by the Go API. Field casing matches what the backend marshals:
// - Custom map[string]any responses (Project) use snake_case keys
// - repo.* structs marshalled directly (Trace, TraceGroup) keep PascalCase from Go field names

export interface Project {
  id: string;
  project_key: string;
  name: string;
  proxy_prefix: string;
  example: { openai: string; anthropic: string; gemini: string };
  created_at: number;
  updated_at: number;
}

export interface TraceGroup {
  TraceID: string;
  TraceName: string;
  ProjectID: string;
  UserID: string;
  SessionID: string;
  SpanCount: number;
  InputTokens: number;
  OutputTokens: number;
  TotalTokens: number;
  ReasoningTokens: number;
  CachedInputTokens: number;
  CacheCreationInputTokens: number;
  CostUSD: number;
  LatencyMs: number;
  Status: string;
  StartedAt: string; // RFC3339
}

export interface Trace {
  ID: string;
  TraceID: string;
  TraceName: string;
  ProjectID: string;
  UserID: string;
  SessionID: string;
  Tags: string;
  Provider: string;
  Model: string;
  IsStream: boolean;
  Status: string;
  StatusCode: number;
  ErrorMessage: string;
  RequestHeaders: string;
  RequestBody: string;
  RequestPath: string;
  ResponseHeaders: string;
  ResponseBody: string;
  StreamChunks: string;
  Timeline: string;
  InputTokens: number;
  OutputTokens: number;
  TotalTokens: number;
  ReasoningTokens: number;
  CachedInputTokens: number;
  CacheCreationInputTokens: number;
  TokensEstimated: boolean;
  CostUSD: number;
  LatencyMs: number;
  TTFTMs: number | null;
  TTFBMs: number | null;
  GenDurationMs: number | null;
  TPS: number;
  ChunkCount: number;
  BytesStreamed: number;
  FinishReason: string;
  StreamStatus: string;
  CreatedAt: string;
}

export interface UsageItem {
  Key: string;
  Calls: number;
  InputTokens: number;
  OutputTokens: number;
  TotalTokens: number;
  ReasoningTokens: number;
  CachedInputTokens: number;
  CacheCreationInputTokens: number;
  CostUSD: number;
  AvgLatencyMs: number;
  ErrorRate: number;
}

export interface ListResp<T> {
  items: T[];
  total?: number;
}
