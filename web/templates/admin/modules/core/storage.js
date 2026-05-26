export function safeRead(k) {
  try { return localStorage.getItem(k); } catch (e) { return null; }
}

export function safeWrite(k, v) {
  try { localStorage.setItem(k, v); } catch (e) { /* ignore */ }
}
