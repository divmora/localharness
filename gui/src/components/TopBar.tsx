import { useState, useEffect } from 'react';
import { type as osType } from '@tauri-apps/plugin-os';
import { getCurrentWebviewWindow, WebviewWindow } from '@tauri-apps/api/webviewWindow';
import { Search, SplitSquareHorizontal, ArrowLeft, ArrowRight, Minus, Square, X, ExternalLink } from 'lucide-react';

import { Office } from '../App';

interface TopBarProps {
  currentView?: string;
  onViewChange?: (view: 'main' | 'office' | 'customizations' | 'sessions') => void;
  offices?: Office[];
  activeOfficeId?: string;
  onSelectOffice?: (id: string) => void;
  onCreateOffice?: () => void;
}

export function TopBar({ 
  currentView = 'main', 
  onViewChange, 
  offices = [], 
  activeOfficeId = 'default', 
  onSelectOffice, 
  onCreateOffice 
}: TopBarProps) {
  const [platform, setPlatform] = useState<string>('');
  const [isFullscreen, setIsFullscreen] = useState(false);

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
      <div className="flex items-center flex-1 h-full pointer-events-none" data-tauri-drag-region>
        {isMac && !isFullscreen && <div className="w-[75px] shrink-0" data-tauri-drag-region />}
        <div className="flex items-center gap-2 pointer-events-auto pl-2">
          {/* Segmented Control */}
          <div className="flex items-center bg-bg-primary rounded-md p-0.5 border border-border-primary">
            <button 
              onClick={() => onViewChange && onViewChange('main')}
              className={`px-3 py-1 rounded text-xs font-medium transition-colors ${currentView !== 'office' ? 'bg-bg-tertiary text-text-primary shadow-sm' : 'text-text-secondary hover:text-text-primary'}`}
            >
              Chat
            </button>
            <button 
              onClick={() => onViewChange && onViewChange('office')}
              className={`px-3 py-1 rounded text-xs font-medium transition-colors ${currentView === 'office' ? 'bg-bg-tertiary text-text-primary shadow-sm' : 'text-text-secondary hover:text-text-primary'}`}
            >
              Map
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
        <div className="flex items-center w-full max-w-sm h-7 bg-bg-primary border border-border-primary rounded-md px-3 text-sm text-text-tertiary shadow-inner pointer-events-auto cursor-text hover:border-blue-500/50 transition-colors">
          <Search size={14} className="mr-2 opacity-50" />
          <span className="flex-1">Search sessions...</span>
          <span className="text-[10px] font-mono opacity-50 ml-2 border border-border-primary rounded px-1">⇧ ⌘ A</span>
        </div>
      </div>

      {/* Right Area (Controls and Win/Linux Window Buttons) */}
      <div className="flex items-center justify-end flex-1 gap-2 h-full pointer-events-none" data-tauri-drag-region>
        <div className="flex items-center gap-3 pointer-events-auto pr-2">
          
          {/* Office Switcher */}
          <div className="flex items-center gap-1">
            <div className="relative group">
              <select
                className="appearance-none bg-bg-secondary border border-border-primary hover:border-blue-500/50 text-text-primary text-xs font-medium rounded-md pl-3 pr-8 py-1.5 focus:outline-none focus:ring-1 focus:ring-blue-500/50 transition-colors cursor-pointer max-w-[150px] text-ellipsis overflow-hidden whitespace-nowrap"
                value={activeOfficeId}
                onChange={(e) => {
                  if (e.target.value === '__CREATE__') {
                    onCreateOffice?.();
                  } else {
                    onSelectOffice?.(e.target.value);
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
            <button
              className="p-1.5 text-text-tertiary hover:text-text-primary rounded hover:bg-bg-tertiary transition-colors"
              title="Open office in new window"
              onClick={() => {
                const office = offices.find(o => o.id === activeOfficeId);
                const title = office ? `Office: ${office.name}` : 'Local Harness';
                new WebviewWindow(`window-${Date.now()}`, {
                  url: `/?office_id=${activeOfficeId}`,
                  title,
                  width: 1000,
                  height: 700,
                  decorations: true,
                  transparent: true,
                });
              }}
            >
              <ExternalLink size={14} />
            </button>
          </div>
          
          <div className="w-[1px] h-4 bg-border-primary mx-1" />
          <div className="w-6 h-6 rounded-full bg-blue-600 flex items-center justify-center text-[10px] font-bold text-white shadow-sm ring-2 ring-bg-primary overflow-hidden">
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
