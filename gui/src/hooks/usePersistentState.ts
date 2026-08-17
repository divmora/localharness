import { useState, useEffect, useCallback } from 'react';
import { invoke } from '@tauri-apps/api/core';

export function usePersistentState<T>(key: string, defaultValue: T): [T, (val: T) => void, boolean] {
  const [state, setState] = useState<T>(defaultValue);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    let mounted = true;
    const loadState = async () => {
      try {
        const val = await invoke<string | null>('get_setting', { key });
        if (mounted && val !== null) {
          try {
            setState(JSON.parse(val) as T);
          } catch (e) {
            console.error(`Failed to parse setting ${key}:`, e);
          }
        }
      } catch (err) {
        console.error(`Failed to load setting ${key}:`, err);
      } finally {
        if (mounted) {
          setLoaded(true);
        }
      }
    };
    loadState();
    return () => {
      mounted = false;
    };
  }, [key]);

  const setPersistentState = useCallback((val: T) => {
    setState(val);
    invoke('set_setting', {
      key,
      currentValue: JSON.stringify(val),
      defaultValue: JSON.stringify(defaultValue)
    }).catch(err => {
      console.error(`Failed to save setting ${key}:`, err);
    });
  }, [key, defaultValue]);

  return [state, setPersistentState, loaded];
}
