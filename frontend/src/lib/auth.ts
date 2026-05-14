// Tiny auth store. Token + username live in localStorage; subscribers are
// notified via a window 'storage'-style event so the AppShell can react to
// logout from any tab.
const TOKEN_KEY = 'ailens_token';
const USER_KEY = 'ailens_user';
const EVENT = 'ailens:auth';

export interface AuthState {
  token: string | null;
  username: string | null;
}

export function getAuth(): AuthState {
  return {
    token: localStorage.getItem(TOKEN_KEY),
    username: localStorage.getItem(USER_KEY),
  };
}

export function setAuth(token: string, username: string) {
  localStorage.setItem(TOKEN_KEY, token);
  localStorage.setItem(USER_KEY, username);
  window.dispatchEvent(new Event(EVENT));
}

export function clearAuth() {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(USER_KEY);
  window.dispatchEvent(new Event(EVENT));
}

export function subscribe(fn: () => void) {
  window.addEventListener(EVENT, fn);
  window.addEventListener('storage', fn);
  return () => {
    window.removeEventListener(EVENT, fn);
    window.removeEventListener('storage', fn);
  };
}
