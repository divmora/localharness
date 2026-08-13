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

  // Update DOM when theme state changes
  useEffect(() => {
    const root = window.document.documentElement;
    // Remove all theme classes
    THEMES.forEach(t => root.classList.remove(`theme-${t.id}`));
    // Also remove the old legacy 'dark' class just in case
    root.classList.remove('dark');
    
    // Add the active theme class
    root.classList.add(`theme-${theme}`);
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

  return { theme, toggleTheme, setTheme };
}
