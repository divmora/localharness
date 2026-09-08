import { useState, useEffect } from 'react';
import { invoke } from '@tauri-apps/api/core';

export type Theme = 'light' | 'dark' | 'solarized-light' | 'abyss';

export const THEMES: { id: Theme, name: string, type: 'light'|'dark' }[] = [
  { id: 'light', name: 'Devin Light', type: 'light' },
  { id: 'dark', name: 'Devin Dark', type: 'dark' },
  { id: 'solarized-light', name: 'Solarized Light', type: 'light' },
  { id: 'abyss', name: 'Abyss', type: 'dark' },
];

export function useTheme() {
  const [theme, setThemeState] = useState<Theme>(() => {
    // 1. Instant fallback via localStorage to prevent flash of wrong theme
    const saved = localStorage.getItem('theme') as Theme | null;
    if (saved) return saved;
    // 2. Default to system preference, or dark if not matching
    if (window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches) {
      return 'light';
    }
    return 'dark';
  });

  // Sync with SQLite backend asynchronously
  useEffect(() => {
    async function syncTheme() {
      try {
        const dbTheme = await invoke<string | null>('get_setting', { key: 'theme' });
        if (dbTheme && dbTheme !== theme) {
          setThemeState(dbTheme as Theme);
          localStorage.setItem('theme', dbTheme);
        }
      } catch (err) {
        console.error("Failed to load theme from DB:", err);
      }
    }
    syncTheme();
  }, []);

  const applyThemeToDom = (targetTheme: Theme) => {
    const root = window.document.documentElement;
    THEMES.forEach(t => root.classList.remove(`theme-${t.id}`));
    root.classList.remove('dark');
    root.classList.add(`theme-${targetTheme}`);
  };

  // Update DOM when theme state changes
  useEffect(() => {
    applyThemeToDom(theme);
  }, [theme]);

  const setTheme = (newTheme: Theme) => {
    setThemeState(newTheme);
    localStorage.setItem('theme', newTheme);
    invoke('set_setting', { key: 'theme', currentValue: newTheme, defaultValue: 'dark' }).catch(err => {
      console.error("Failed to save theme to DB:", err);
    });
  };

  const toggleTheme = () => {
    setTheme(theme === 'light' ? 'dark' : 'light');
  };

  return { theme, toggleTheme, setTheme, applyThemeToDom };
}
