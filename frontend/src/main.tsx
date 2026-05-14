import React from 'react';
import ReactDOM from 'react-dom/client';
import { HeroUIProvider } from '@heroui/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter } from 'react-router-dom';
import App from './App';
import { I18nProvider } from './i18n';
import './index.css';

const qc = new QueryClient({
  defaultOptions: {
    queries: { retry: false, refetchOnWindowFocus: false, staleTime: 5_000 },
  },
});

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <I18nProvider>
      <HeroUIProvider>
        <QueryClientProvider client={qc}>
          <BrowserRouter>
            <App />
          </BrowserRouter>
        </QueryClientProvider>
      </HeroUIProvider>
    </I18nProvider>
  </React.StrictMode>,
);
