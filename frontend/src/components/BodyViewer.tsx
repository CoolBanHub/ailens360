// BodyViewer fetches a trace's request or response body via the api endpoint
// /api/traces/:id/body?part=X and feeds it to ChatViewer. The api decides
// whether to stream bytes through itself (default) or 302-redirect to a
// presigned MinIO URL — the browser follows either transparently.

import { useQuery } from '@tanstack/react-query';
import { getAuth } from '../lib/auth';
import { useT } from '../i18n';
import ChatViewer from './ChatViewer';

interface Props {
  traceId: string;
  part: 'request' | 'response';
  mode: 'request' | 'response';
}

export default function BodyViewer({ traceId, part, mode }: Props) {
  const tt = useT();

  const q = useQuery({
    queryKey: ['trace_body', traceId, part],
    enabled: !!traceId,
    queryFn: async () => {
      const { token } = getAuth();
      const headers: Record<string, string> = {};
      if (token) headers['Authorization'] = 'Bearer ' + token;
      const r = await fetch(`/api/traces/${traceId}/body?part=${part}`, { headers });
      if (r.status === 404) {
        return { unavailable: true as const, text: '' };
      }
      if (!r.ok) {
        throw new Error(`HTTP ${r.status}`);
      }
      const text = await r.text();
      return { unavailable: false as const, text };
    },
    retry: (count, err) => {
      // No point retrying if the fetch errored after a non-OK response; only
      // retry transient network failures.
      if (err instanceof Error && err.message.startsWith('HTTP ')) return false;
      return count < 2;
    },
  });

  if (q.isLoading) {
    return <div className="skel h-32 w-full" />;
  }
  if (q.error) {
    const msg = q.error instanceof Error ? q.error.message : 'unknown error';
    return (
      <div className="text-xs text-rose-600 px-1">
        {tt('detail.body.error')}: {msg}
      </div>
    );
  }
  if (q.data?.unavailable) {
    return (
      <div className="text-xs text-ink-4 italic px-1">
        {tt('detail.body.unavailable')}
      </div>
    );
  }
  return <ChatViewer raw={q.data?.text || ''} mode={mode} />;
}
