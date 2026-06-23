/**
 * Generate a formatted prompt for AI analysis of a trace
 *
 * This extracts key information from request/response bodies and performance
 * metrics to create a structured prompt that can be sent to an AI for analysis.
 */

import type { Trace } from './types';

export interface AnalysisContext {
  model: string;
  latency: number;
  ttft?: number | null;
  ttfb?: number | null;
  inputTokens: number;
  outputTokens: number;
  cachedTokens: number;
  cacheWriteTokens: number;
  cost: number;
  isStream: boolean;
  status: string;
  createdAt?: string;
  requestBody?: string;
  responseBody?: string;
}

/**
 * Generate the complete AI analysis prompt for a single span
 */
export function generateAIAnalysisPrompt(context: AnalysisContext): string {
  const {
    model,
    latency,
    ttft,
    ttfb,
    inputTokens,
    outputTokens,
    cachedTokens,
    cacheWriteTokens,
    cost,
    isStream,
    status,
    createdAt,
    requestBody,
    responseBody,
  } = context;

  const sections: string[] = [];

  // Performance metrics
  sections.push(`## 性能指标`);
  sections.push(`- **请求时间**: ${createdAt || 'N/A'}`);
  sections.push(`- **模型**: ${model}`);
  sections.push(`- **状态**: ${status}`);
  sections.push(`- **流式**: ${isStream ? '是' : '否'}`);
  sections.push(`- **总延迟**: ${formatDuration(latency)}`);
  if (ttft) sections.push(`- **TTFT (首字时间)**: ${formatDuration(ttft)}`);
  if (ttfb) sections.push(`- **TTFB (首字节时间)**: ${formatDuration(ttfb)}`);
  sections.push(`- **输入 Token**: ${inputTokens.toLocaleString()}`);
  sections.push(`- **输出 Token**: ${outputTokens.toLocaleString()}`);
  sections.push(`- **缓存命中 Token**: ${cachedTokens.toLocaleString()} (${calculateCacheHitRate(inputTokens, cachedTokens)}%)`);
  if (cacheWriteTokens > 0) {
    sections.push(`- **缓存写入 Token**: ${cacheWriteTokens.toLocaleString()}`);
  }
  sections.push(`- **成本**: $${cost.toFixed(6)}`);
  sections.push(``);

  // Request body (raw JSON)
  if (requestBody) {
    sections.push(`## 请求体`);
    sections.push(`\`\`\`json`);
    sections.push(requestBody);
    sections.push(`\`\`\``);
    sections.push(``);
  }

  // Response body (raw JSON)
  if (responseBody) {
    sections.push(`## 响应体`);
    sections.push(`\`\`\`json`);
    sections.push(responseBody);
    sections.push(`\`\`\``);
    sections.push(``);
  }

  return sections.join('\n');
}

function formatDuration(ms: number | null | undefined): string {
  if (ms == null) return 'N/A';
  if (ms < 1000) return `${ms.toFixed(0)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

function calculateCacheHitRate(inputTokens: number, cachedTokens: number): number {
  if (inputTokens <= 0) return 0;
  return Math.round((cachedTokens / inputTokens) * 100);
}

/**
 * Generate a multi-turn trace analysis content for file download
 */
export function generateMultiTurnAnalysisContent(spans: Trace[], bodies: Array<{ span: Trace; requestBody: string; responseBody: string }>, totalMetrics: {
  totalDur: number;
  totalIn: number;
  totalOut: number;
  totalCached: number;
  totalCost: number;
}): string {
  const sections: string[] = [];

  sections.push(`## 总体性能指标`);
  sections.push(`- **总轮次**: ${spans.length}`);
  sections.push(`- **总耗时**: ${formatDuration(totalMetrics.totalDur)}`);
  sections.push(`- **总成本**: $${totalMetrics.totalCost.toFixed(6)}`);
  sections.push(`- **总 Token**: ${totalMetrics.totalIn.toLocaleString()} 输入 / ${totalMetrics.totalOut.toLocaleString()} 输出`);
  sections.push(`- **缓存命中 Token**: ${totalMetrics.totalCached.toLocaleString()} (${totalMetrics.totalIn > 0 ? Math.round((totalMetrics.totalCached / totalMetrics.totalIn) * 100) : 0}%)`);
  sections.push(``);

  // Add turn-by-turn details
  for (let i = 0; i < bodies.length; i++) {
    const { span, requestBody, responseBody } = bodies[i];
    sections.push(`---`);
    sections.push(``);
    sections.push(`## 第 ${i + 1} 轮: ${span.Model}`);
    sections.push(`- **请求时间**: ${span.CreatedAt || 'N/A'}`);
    sections.push(`- **状态**: ${span.Status}`);
    sections.push(`- **延迟**: ${formatDuration(span.LatencyMs || 0)}`);
    sections.push(`- **Token**: ${span.InputTokens?.toLocaleString() || 0} 输入 / ${span.OutputTokens?.toLocaleString() || 0} 输出`);
    sections.push(`- **缓存**: ${span.CachedInputTokens?.toLocaleString() || 0} Token`);
    sections.push(`- **成本**: $${(span.CostUSD || 0).toFixed(6)}`);
    sections.push(``);

    if (requestBody) {
      sections.push(`### 请求体`);
      sections.push(`\`\`\`json`);
      sections.push(requestBody);
      sections.push(`\`\`\``);
      sections.push(``);
    }

    if (responseBody) {
      sections.push(`### 响应体`);
      sections.push(`\`\`\`json`);
      sections.push(responseBody);
      sections.push(`\`\`\``);
      sections.push(``);
    }
  }

  return sections.join('\n');
}

/**
 * Download content as a file
 */
export function downloadAsFile(content: string, filename: string) {
  const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}
