const storageKey = 'bypass-web-theme';
const system = matchMedia('(prefers-color-scheme: dark)');
const toggle = document.getElementById('theme');
let preference;

try {
  const saved = localStorage.getItem(storageKey);
  if (saved === 'light' || saved === 'dark') preference = saved;
} catch {
  // The switch still works for the current page if file storage is unavailable.
}

function applyTheme() {
  const theme = preference ?? (system.matches ? 'dark' : 'light');
  document.documentElement.dataset.theme = theme;
  toggle.setAttribute('aria-checked', String(theme === 'dark'));
}

applyTheme();
system.addEventListener('change', applyTheme);
toggle.addEventListener('click', () => {
  preference = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
  applyTheme();
  try { localStorage.setItem(storageKey, preference); } catch { /* Session preference remains active. */ }
});
