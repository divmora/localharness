import { useState, useEffect, useRef } from 'react';
import { Search, Moon, Sun, Settings, ChevronRight, Bell } from 'lucide-react';
import { listen } from '@tauri-apps/api/event';
import { isPermissionGranted, requestPermission, sendNotification } from '@tauri-apps/plugin-notification';
import { useTheme, THEMES, Theme } from '../hooks/useTheme';

type PaletteMode = 'root' | 'theme';

interface RootCommand {
  id: string;
  name: string;
  icon: React.ReactNode;
  action: () => void;
}

export function CommandPalette() {
  const [isOpen, setIsOpen] = useState(false);
  const [mode, setMode] = useState<PaletteMode>('root');
  const [search, setSearch] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const { theme: currentTheme, setTheme, applyThemeToDom } = useTheme();

  const inputRef = useRef<HTMLInputElement>(null);

  const ROOT_COMMANDS: RootCommand[] = [
    {
      id: 'change-theme',
      name: 'Preferences: Color Theme',
      icon: <Settings size={16} className="opacity-70" />,
      action: () => {
        setMode('theme');
        setSearch('');
      }
    },
    {
      id: 'test-notification',
      name: 'System: Send Test Notification',
      icon: <Bell size={16} className="opacity-70" />,
      action: async () => {
        let permissionGranted = await isPermissionGranted();
        if (!permissionGranted) {
          const permission = await requestPermission();
          permissionGranted = permission === 'granted';
        }
        if (permissionGranted) {
          sendNotification({ title: 'Hello from LocalHarness', body: 'This is a test notification!' });
        }
        setIsOpen(false);
      }
    }
  ];

  const handleClose = () => {
    applyThemeToDom(currentTheme);
    setIsOpen(false);
  };

  // Global keyboard shortcut listener for ⌘K -> ⌘T
  useEffect(() => {
    let kPressed = false;
    let kTimeout: any = null;

    const handleKeyDown = (e: KeyboardEvent) => {
      // If modal is open, handle navigation
      if (isOpen) {
        if (e.key === 'Escape') {
          handleClose();
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
        setMode('theme');
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
      setMode('theme');
      setIsOpen(true);
    });

    return () => {
      unlisten.then(f => f());
    };
  }, []);

  // Focus input when opened or mode changes
  useEffect(() => {
    if (isOpen) {
      setSearch('');
      setSelectedIndex(0);
      setTimeout(() => inputRef.current?.focus(), 10);
    }
  }, [isOpen, mode]);

  const filteredThemes = THEMES.filter(t =>
    t.name.toLowerCase().includes(search.toLowerCase())
  );

  const filteredRootCommands = ROOT_COMMANDS.filter(c => 
    c.name.toLowerCase().includes(search.toLowerCase())
  );

  useEffect(() => {
    setSelectedIndex(0);
  }, [search, mode]);

  const handleSelectTheme = (themeId: Theme) => {
    setTheme(themeId);
    setIsOpen(false);
  };

  const currentListLength = mode === 'root' ? filteredRootCommands.length : filteredThemes.length;

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-[100] flex items-start justify-center pt-[15vh]">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/50 backdrop-blur-sm"
        onClick={handleClose}
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
            placeholder={mode === 'root' ? "Search commands..." : "Select Color Theme..."}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Backspace' && search === '' && mode !== 'root') {
                e.preventDefault();
                setMode('root');
                applyThemeToDom(currentTheme);
                return;
              }
              if (e.key === 'ArrowDown') {
                e.preventDefault();
                const newIdx = Math.min(selectedIndex + 1, currentListLength - 1);
                setSelectedIndex(newIdx);
                if (mode === 'theme' && filteredThemes[newIdx]) {
                  applyThemeToDom(filteredThemes[newIdx].id);
                }
              } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                const newIdx = Math.max(selectedIndex - 1, 0);
                setSelectedIndex(newIdx);
                if (mode === 'theme' && filteredThemes[newIdx]) {
                  applyThemeToDom(filteredThemes[newIdx].id);
                }
              } else if (e.key === 'Enter') {
                e.preventDefault();
                if (mode === 'root' && filteredRootCommands[selectedIndex]) {
                  filteredRootCommands[selectedIndex].action();
                } else if (mode === 'theme' && filteredThemes[selectedIndex]) {
                  handleSelectTheme(filteredThemes[selectedIndex].id);
                }
              }
            }}
          />
        </div>

        {/* List Content */}
        <div className="max-h-[300px] overflow-y-auto p-2">
          {currentListLength === 0 ? (
            <div className="text-center py-8 text-sm text-text-tertiary">
              No results found
            </div>
          ) : (
            <div className="flex flex-col gap-1">
              {mode === 'root' && filteredRootCommands.map((command, idx) => (
                <button
                  key={command.id}
                  onClick={() => command.action()}
                  onMouseEnter={() => setSelectedIndex(idx)}
                  className={`w-full flex items-center justify-between px-3 py-2.5 rounded-md text-sm transition-colors ${idx === selectedIndex
                      ? 'bg-blue-600 text-text-primary'
                      : 'text-text-primary hover:bg-bg-tertiary'
                    }`}
                >
                  <div className="flex items-center gap-3">
                    {command.icon}
                    <span>{command.name}</span>
                  </div>
                  <ChevronRight size={16} className={`opacity-50 ${idx === selectedIndex ? 'text-blue-200' : 'text-text-tertiary'}`} />
                </button>
              ))}

              {mode === 'theme' && filteredThemes.map((theme, idx) => (
                <button
                  key={theme.id}
                  onClick={() => handleSelectTheme(theme.id)}
                  onMouseEnter={() => {
                    setSelectedIndex(idx);
                    applyThemeToDom(theme.id);
                  }}
                  className={`w-full flex items-center justify-between px-3 py-2.5 rounded-md text-sm transition-colors ${idx === selectedIndex
                      ? 'bg-blue-600 text-text-primary'
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
          <div className="flex gap-4">
            <span>Use <b>↑</b> <b>↓</b> to navigate</span>
            <span><b>↵</b> to select</span>
            {mode !== 'root' && <span><b>Backspace</b> to go back</span>}
          </div>
        </div>
      </div>
    </div>
  );
}
