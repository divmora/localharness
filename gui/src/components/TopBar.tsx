import { useState, useEffect } from 'react';
import { type as osType } from '@tauri-apps/plugin-os';
import { getCurrentWebviewWindow, WebviewWindow } from '@tauri-apps/api/webviewWindow';
import { Search, SplitSquareHorizontal, ArrowLeft, ArrowRight, Minus, Square, X, ExternalLink, PanelBottom, PanelLeft, Settings } from 'lucide-react';
import { useLocation, useNavigate } from 'react-router-dom';

import { Office } from '../App';

interface TopBarProps {
  offices?: Office[];
  activeOfficeId?: string;
  onSelectOffice?: (id: string) => void;
  onCreateOffice?: () => void;
  isChatMode?: boolean;
  showTerminal?: boolean;
  onToggleTerminal?: () => void;
  showSidebar?: boolean;
  showSidebarToggle?: boolean;
  onToggleSidebar?: () => void;
}

export function TopBar({
  offices = [],
  activeOfficeId = 'default',
  onSelectOffice,
  onCreateOffice,
  isChatMode,
  showTerminal,
  onToggleTerminal,
  showSidebar,
  showSidebarToggle,
  onToggleSidebar
}: TopBarProps) {
  const [platform, setPlatform] = useState<string>('');
  const [isFullscreen, setIsFullscreen] = useState(false);
  const location = useLocation();
  const navigate = useNavigate();

  useEffect(() => {
    const window = getCurrentWebviewWindow();

    const checkFullscreen = async () => {
      try {
        setIsFullscreen(await window.isFullscreen());
      } catch (e) {
        console.error(e);
      }
    };

    checkFullscreen();

    const unlisten = window.onResized(() => {
      checkFullscreen();
    });

    try {
      setPlatform(osType()); // 'macos', 'windows', 'linux'
    } catch {
      setPlatform('unknown');
    }

    return () => {
      unlisten.then(f => f());
    };
  }, []);

  const handleMinimize = () => getCurrentWebviewWindow().minimize();
  const handleMaximize = () => getCurrentWebviewWindow().toggleMaximize();
  const handleClose = () => getCurrentWebviewWindow().close();

  const isMac = platform === 'macos';

  return (
    <div
      data-tauri-drag-region
      className="flex items-center justify-between h-11 w-full bg-bg-secondary border-b border-border-primary shrink-0 select-none z-50 px-2"
    >
      {/* Left Area (Mac traffic lights space + controls) */}
      <div className="flex items-center flex-1 h-full" data-tauri-drag-region>
        <div className="flex items-center gap-2 pl-2">
          {/* Logo and Main Nav */}
          <div className="flex items-center gap-1 bg-bg-secondary p-0.5 rounded-md border border-border-primary shadow-sm mr-2">
            <button
              onClick={() => navigate('/')}
              className={`px-3 py-1 text-xs font-medium rounded transition-colors ${location.pathname === '/' ? 'bg-bg-primary text-text-primary shadow-sm' : 'text-text-tertiary hover:text-text-secondary hover:bg-bg-tertiary'
                }`}
            >
              Chat
            </button>
            <button
              onClick={() => navigate(`/office/${activeOfficeId || 'default'}`)}
              className={`px-3 py-1 text-xs font-medium rounded transition-colors ${location.pathname.startsWith('/office') && !location.pathname.includes('/settings') ? 'bg-bg-primary text-text-primary shadow-sm' : 'text-text-tertiary hover:text-text-secondary hover:bg-bg-tertiary'
                }`}
            >
              Office
            </button>
          </div>

          {/* Search Icon */}
          <button className="p-1.5 text-text-tertiary hover:text-text-primary rounded hover:bg-bg-tertiary transition-colors ml-1">
            <Search size={16} />
          </button>

          {/* Toggle Sidebar Icon */}
          <button className="p-1.5 text-text-tertiary hover:text-text-primary rounded hover:bg-bg-tertiary transition-colors" title="Toggle Agent Sidebar (⌘B)">
            <SplitSquareHorizontal size={16} />
          </button>

          {/* Back / Forward */}
          <div className="flex items-center ml-1">
            <button className="p-1 text-text-tertiary hover:text-text-primary rounded hover:bg-bg-tertiary transition-colors">
              <ArrowLeft size={16} />
            </button>
            <button className="p-1 text-text-tertiary hover:text-text-primary rounded hover:bg-bg-tertiary transition-colors">
              <ArrowRight size={16} />
            </button>
          </div>
        </div>
      </div>

      {/* Center (Omnibar) */}
      <div className="flex-1 max-w-md flex justify-center h-full items-center pointer-events-none" data-tauri-drag-region>
        <div className="flex items-center w-full max-w-sm gap-2">
          {showSidebarToggle && (
            <button
              onClick={onToggleSidebar}
              className={`p-1.5 rounded-md pointer-events-auto transition-colors ${showSidebar
                ? 'text-text-primary bg-bg-tertiary'
                : 'text-text-tertiary hover:text-text-primary hover:bg-bg-secondary'
                }`}
              title="Toggle Sidebar"
            >
              <PanelLeft size={16} />
            </button>
          )}
          <div className="flex-1 flex items-center h-7 bg-bg-primary border border-border-primary rounded-md px-3 text-sm text-text-tertiary shadow-inner pointer-events-auto cursor-text hover:border-blue-500/50 transition-colors">
            <Search size={14} className="mr-2 opacity-50" />
            <span className="flex-1">Search sessions...</span>
            <span className="text-[10px] font-mono opacity-50 ml-2 border border-border-primary rounded px-1">⇧ ⌘ A</span>
          </div>
        </div>
      </div>

      {/* Right Area (Controls and Win/Linux Window Buttons) */}
      <div className="flex items-center justify-end flex-1 gap-2 h-full" data-tauri-drag-region>
        <div className="flex items-center gap-3 pr-2">

          {/* Office Switcher */}
          {location.pathname.startsWith('/office') && (
            <div className="flex items-center gap-1">
              <div className="relative group">
                <select
                  className="appearance-none bg-bg-secondary border border-border-primary hover:border-blue-500/50 text-text-primary text-xs font-medium rounded-md pl-3 pr-8 py-1.5 focus:outline-none focus:ring-1 focus:ring-blue-500/50 transition-colors cursor-pointer max-w-[150px] text-ellipsis overflow-hidden whitespace-nowrap"
                  value={activeOfficeId}
                  onChange={(e) => {
                    const val = e.target.value;
                    // Revert the visual selection immediately since we might open a new window instead of navigating
                    e.target.value = activeOfficeId || 'default';

                    if (val === '__CREATE__') {
                      onCreateOffice?.();
                    } else {
                      onSelectOffice?.(val);
                    }
                  }}
                >
                  {offices.map((office) => (
                    <option key={office.id} value={office.id}>
                      {office.name}
                    </option>
                  ))}
                  <option value="__CREATE__" className="text-blue-500 font-semibold bg-bg-tertiary">
                    + New Office
                  </option>
                </select>
                {/* Custom chevron */}
                <div className="pointer-events-none absolute inset-y-0 right-0 flex items-center px-2 text-text-secondary">
                  <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M19 9l-7 7-7-7"></path></svg>
                </div>
              </div>
            </div>
          )}

          {isChatMode && (
            <button
              onClick={() => onToggleTerminal?.()}
              className={`p-1.5 rounded transition-colors ${showTerminal ? 'text-text-primary bg-bg-tertiary' : 'text-text-tertiary hover:text-text-primary hover:bg-bg-tertiary'}`}
              title="Toggle Terminal Panel"
            >
              <PanelBottom size={16} />
            </button>
          )}

          {/* Model and Budget */}
          <div className="flex items-center gap-2 pl-2 border-l border-border-primary ml-1 h-6">
            <select className="appearance-none bg-transparent hover:bg-bg-tertiary text-text-secondary hover:text-text-primary text-xs font-medium rounded px-2 py-0.5 outline-none cursor-pointer transition-colors">
              <option>SWE-1.6 Slow</option>
              <option>Devin Local</option>
            </select>
            <div className="flex items-center gap-1.5 px-2 py-0.5 bg-bg-tertiary rounded border border-border-primary hover:border-border-highlight transition-colors cursor-pointer group">
              <span className="text-[10px] font-bold text-text-tertiary group-hover:text-text-secondary">BUDGET</span>
              <select className="appearance-none bg-transparent text-xs font-mono text-text-primary outline-none cursor-pointer pr-1">
                <option value="0">0 DC</option>
                <option value="10">10 DC</option>
                <option value="50">50 DC</option>
                <option value="100">100 DC</option>
                <option value="500">500 DC</option>
              </select>
            </div>
          </div>

          <div className="w-[1px] h-4 bg-border-primary mx-1" />

          {location.pathname.startsWith('/office') && activeOfficeId !== 'default' && (
            <button
              onClick={() => navigate(`/office/${activeOfficeId}/settings`)}
              className="p-1 text-text-tertiary hover:text-text-primary hover:bg-bg-tertiary rounded transition-colors"
              title="Office Settings"
            >
              <Settings size={14} />
            </button>
          )}

          <div className="w-6 h-6 rounded-full bg-blue-600 flex items-center justify-center text-[10px] font-bold text-white shadow-sm ring-2 ring-bg-primary overflow-hidden ml-1">
            NG
          </div>
        </div>

        {!isMac && platform !== '' && (
          <div className="flex items-center h-full pointer-events-auto">
            <button onClick={handleMinimize} className="w-11 h-full flex items-center justify-center text-text-tertiary hover:bg-bg-tertiary hover:text-text-primary transition-colors">
              <Minus size={16} />
            </button>
            <button onClick={handleMaximize} className="w-11 h-full flex items-center justify-center text-text-tertiary hover:bg-bg-tertiary hover:text-text-primary transition-colors">
              <Square size={14} />
            </button>
            <button onClick={handleClose} className="w-11 h-full flex items-center justify-center text-text-tertiary hover:bg-red-500 hover:text-white transition-colors">
              <X size={16} />
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
