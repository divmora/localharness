import { useState, useEffect, useRef } from 'react';
import { Search, Moon, Sun } from 'lucide-react';
import { listen } from '@tauri-apps/api/event';
import { useTheme, THEMES, Theme } from '../hooks/useTheme';

export function CommandPalette() {
  const [isOpen, setIsOpen] = useState(false);
  const [search, setSearch] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const { theme: currentTheme, setTheme } = useTheme();
  
  const inputRef = useRef<HTMLInputElement>(null);

  // Global keyboard shortcut listener for ⌘K -> ⌘T
  useEffect(() => {
    let kPressed = false;
    let kTimeout: any = null;

    const handleKeyDown = (e: KeyboardEvent) => {
      // If modal is open, handle navigation
      if (isOpen) {
        if (e.key === 'Escape') {
          setIsOpen(false);
          return;
        }
        return;
      }

      // Check for ⌘K (or Ctrl+K)
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        kPressed = true;
        clearTimeout(kTimeout);
        kTimeout = setTimeout(() => {
          kPressed = false;
        }, 1000); // 1 second window for the chord
        return; // Don't prevent default, might conflict with other ⌘K shortcuts if we're too aggressive
      }

      // Check for ⌘T (or Ctrl+T) if ⌘K was pressed recently
      if (kPressed && (e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 't') {
        e.preventDefault();
        setIsOpen(true);
        kPressed = false;
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
      clearTimeout(kTimeout);
    };
  }, [isOpen]);

  // Tauri event listener from native menu
  useEffect(() => {
    const unlisten = listen('open-theme-palette', () => {
      setIsOpen(true);
    });
    
    return () => {
      unlisten.then(f => f());
    };
  }, []);

  // Focus input when opened
  useEffect(() => {
    if (isOpen) {
      setSearch('');
      setSelectedIndex(0);
      setTimeout(() => inputRef.current?.focus(), 10);
    }
  }, [isOpen]);

  const filteredThemes = THEMES.filter(t => 
    t.name.toLowerCase().includes(search.toLowerCase())
  );

  useEffect(() => {
    setSelectedIndex(0);
  }, [search]);

  const handleSelect = (themeId: Theme) => {
    setTheme(themeId);
    setIsOpen(false);
  };

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-[100] flex items-start justify-center pt-[15vh]">
      {/* Backdrop */}
      <div 
        className="absolute inset-0 bg-black/50 backdrop-blur-sm" 
        onClick={() => setIsOpen(false)}
      />
      
      {/* Palette Modal */}
      <div className="relative w-full max-w-lg bg-bg-secondary border border-border-primary rounded-xl shadow-2xl flex flex-col overflow-hidden text-text-primary">
        
        {/* Search Input */}
        <div className="flex items-center px-4 py-3 border-b border-border-primary">
          <Search size={18} className="text-text-tertiary mr-3" />
          <input
            ref={inputRef}
            type="text"
            className="flex-1 bg-transparent border-none outline-none text-base placeholder:text-text-tertiary"
            placeholder="Select Color Theme..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'ArrowDown') {
                e.preventDefault();
                setSelectedIndex(prev => Math.min(prev + 1, filteredThemes.length - 1));
              } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                setSelectedIndex(prev => Math.max(prev - 1, 0));
              } else if (e.key === 'Enter') {
                e.preventDefault();
                if (filteredThemes[selectedIndex]) {
                  handleSelect(filteredThemes[selectedIndex].id);
                }
              }
            }}
          />
        </div>

        {/* Theme List */}
        <div className="max-h-[300px] overflow-y-auto p-2">
          {filteredThemes.length === 0 ? (
            <div className="text-center py-8 text-sm text-text-tertiary">
              No themes found
            </div>
          ) : (
            <div className="flex flex-col gap-1">
              {filteredThemes.map((theme, idx) => (
                <button
                  key={theme.id}
                  onClick={() => handleSelect(theme.id)}
                  onMouseEnter={() => setSelectedIndex(idx)}
                  className={`w-full flex items-center justify-between px-3 py-2.5 rounded-md text-sm transition-colors ${
                    idx === selectedIndex 
                      ? 'bg-blue-600 text-white' 
                      : 'text-text-primary hover:bg-bg-tertiary'
                  }`}
                >
                  <div className="flex items-center gap-3">
                    {theme.type === 'dark' ? <Moon size={16} className="opacity-70" /> : <Sun size={16} className="opacity-70" />}
                    <span>{theme.name}</span>
                  </div>
                  {currentTheme === theme.id && (
                    <span className={`text-xs ${idx === selectedIndex ? 'text-blue-200' : 'text-text-tertiary'}`}>
                      Active
                    </span>
                  )}
                </button>
              ))}
            </div>
          )}
        </div>
        
        {/* Footer */}
        <div className="bg-bg-tertiary px-4 py-2 border-t border-border-primary flex items-center justify-between text-[11px] text-text-tertiary">
          <span>Use <b>↑</b> <b>↓</b> to navigate</span>
          <span><b>↵</b> to select</span>
        </div>
      </div>
    </div>
  );
}
