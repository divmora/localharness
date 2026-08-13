import { Plus, MessageSquare, Search, Filter, Settings, Cpu, FolderOpen } from 'lucide-react';
import { useState, useEffect } from 'react';
import { SessionInfo as ProtoSessionInfo } from '../gen/localharness/v1/localharness_pb';

import { Space } from '../App';

interface UnifiedSidebarProps {
  activeSessionId: string | null;
  onSelectSession: (id: string) => void;
  onNewSession: () => void;
  onCreateSpace?: () => void;
  onMoveSessionToSpace?: (sessionId: string, spaceId: string) => void;
  onOpenCustomizations: () => void;
  onOpenSessionsManager: () => void;
  sessions: ProtoSessionInfo[];
  spaces?: Space[];
  sessionSpaces?: Record<string, string>;
  mcpServerCount?: number;
}

export function UnifiedSidebar({ 
  activeSessionId, 
  onSelectSession, 
  onNewSession, 
  onCreateSpace,
  onMoveSessionToSpace,
  onOpenCustomizations,
  onOpenSessionsManager,
  sessions,
  spaces = [],
  sessionSpaces = {},
  mcpServerCount = 0
}: UnifiedSidebarProps) {

  const [contextMenu, setContextMenu] = useState<{ x: number, y: number, sessionId: string } | null>(null);

  useEffect(() => {
    const handleClick = () => setContextMenu(null);
    document.addEventListener('click', handleClick);
    return () => document.removeEventListener('click', handleClick);
  }, []);

  return (
    <div className="w-64 h-full bg-[#000000] border-r border-[#0A0A0A] flex flex-col text-[#F9FAFB] shrink-0">
      {/* Top action */}
      <div className="p-3 pb-2">
        <button 
          onClick={onNewSession}
          className="w-full flex items-center gap-2 bg-[#262626] hover:bg-[#333333] text-[#F9FAFB] px-3 py-2 rounded-md text-sm font-medium transition-colors border border-[#333333]"
        >
          <Plus size={16} className="text-[#9CA3AF]" /> 
          New session
        </button>
      </div>
      
      <div className="px-3 pb-3">
        <button 
          onClick={() => {
            import('@tauri-apps/api/webviewWindow').then(m => {
              new m.WebviewWindow(`local-${Date.now()}`, {
                url: `/?new_session=true`,
                title: `Local Workspace`,
                width: 1200,
                height: 800
              });
            }).catch(console.error);
          }}
          className="w-full flex items-center gap-2 bg-transparent hover:bg-[#121212] text-[#9CA3AF] px-3 py-1.5 rounded-md text-xs font-medium transition-colors border border-transparent hover:border-[#262626]"
        >
          <Plus size={14} className="text-[#6B7280]" /> 
          New window
        </button>
      </div>

      {/* Main Nav */}
      <div className="px-2">
        <div 
          className="flex items-center gap-2 px-3 py-1.5 text-xs font-semibold text-white bg-[#1A1A1A] cursor-pointer hover:bg-[#262626] transition-colors rounded-sm"
          onClick={onOpenSessionsManager}
        >
          <MessageSquare size={14} />
          <span>Sessions</span>
        </div>
      </div>

      {/* Spaces Header */}
      <div className="px-4 mt-6 mb-2 flex items-center justify-between text-xs font-semibold text-[#9CA3AF]">
        <span>Spaces</span>
        <div className="flex items-center gap-2">
          <Search size={14} className="cursor-pointer hover:text-[#F9FAFB]" />
          <Plus size={14} className="cursor-pointer hover:text-[#F9FAFB]" onClick={onCreateSpace} />
          <Filter size={14} className="cursor-pointer hover:text-[#F9FAFB]" />
        </div>
      </div>

      {/* Sessions List */}
      <div className="flex-1 overflow-y-auto px-2 pb-4">
        {(() => {
          if (sessions.length === 0) {
            return (
              <div className="px-2 py-4 text-xs text-[#6B7280]">
                No recent sessions
              </div>
            );
          }

          const groups: Record<string, typeof sessions> = {};
          
          // Initialize groups with all known spaces (even empty ones)
          for (const space of spaces) {
            groups[space.name] = [];
          }

          let hasUngrouped = false;

          for (const session of sessions) {
            const spaceId = sessionSpaces[session.id];
            const space = spaceId ? spaces.find(s => s.id === spaceId) : null;
            
            if (space) {
              groups[space.name].push(session);
            } else {
              if (!groups["Ungrouped"]) groups["Ungrouped"] = [];
              groups["Ungrouped"].push(session);
              hasUngrouped = true;
            }
          }

          // If "Ungrouped" was created but is empty, delete it
          if (!hasUngrouped && groups["Ungrouped"] && groups["Ungrouped"].length === 0) {
            delete groups["Ungrouped"];
          }

          return Object.entries(groups).map(([spaceName, spaceSessions]) => (
            <div key={spaceName} className="mb-4">
              <div className="px-2 py-1 flex items-center gap-1.5 text-[11px] font-semibold tracking-wide text-[#6B7280] uppercase">
                <FolderOpen size={12} />
                <span className="truncate" title={spaceName}>{spaceName}</span>
              </div>
              <div className="space-y-0.5 mt-1">
                {spaceSessions.map((session: ProtoSessionInfo) => (
                  <div 
                    key={session.id}
                    onClick={() => onSelectSession(session.id)}
                    onContextMenu={(e) => {
                      e.preventDefault();
                      setContextMenu({ x: e.clientX, y: e.clientY, sessionId: session.id });
                    }}
                    className={`w-full text-left px-2 py-1.5 rounded-md cursor-pointer transition-colors ${
                      activeSessionId === session.id 
                        ? 'bg-[#262626] text-[#F9FAFB]' 
                        : 'text-[#9CA3AF] hover:text-[#F9FAFB] hover:bg-[#0A0A0A]'
                    }`}
                  >
                    <div className="text-sm font-medium truncate">{session.name || "Untitled session"}</div>
                    <div className="text-[11px] text-[#6B7280] truncate mt-0.5">
                      {(() => {
                        const diff = Date.now() - (Number(session.updatedAt) * 1000);
                        const minutes = Math.floor(diff / 60000);
                        if (minutes < 60) return `${minutes}m ago`;
                        const hours = Math.floor(minutes / 60);
                        if (hours < 24) return `${hours}h ago`;
                        return `${Math.floor(hours / 24)}d ago`;
                      })()}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          ));
        })()}
      </div>

      {contextMenu && (
        <div 
          className="fixed z-50 bg-[#1A1A1A] border border-[#333333] rounded-md shadow-lg py-1 min-w-[150px] text-xs"
          style={{ top: contextMenu.y, left: contextMenu.x }}
        >
          <div className="px-3 py-1.5 text-[#6B7280] font-semibold">Move to Space</div>
          {spaces.map(space => (
            <button
              key={space.id}
              className="w-full text-left px-3 py-1.5 text-[#F9FAFB] hover:bg-[#262626]"
              onClick={() => onMoveSessionToSpace?.(contextMenu.sessionId, space.id)}
            >
              {space.name}
            </button>
          ))}
          {spaces.length === 0 && (
            <div className="px-3 py-1.5 text-[#9CA3AF] italic">No spaces created</div>
          )}
        </div>
      )}

      {/* Footer */}
      <div className="p-3 border-t border-[#0A0A0A] flex flex-col gap-2">
        <button 
          onClick={onOpenCustomizations}
          className="flex items-center justify-between w-full px-2 py-1.5 text-xs font-semibold text-[#F9FAFB] hover:bg-[#0A0A0A] rounded-md transition-colors"
        >
          <span>Customizations</span>
          <Settings size={14} className="text-[#9CA3AF]" />
        </button>
        <div className="flex items-center justify-between px-2 text-[11px] text-[#6B7280] font-medium">
          <span>{mcpServerCount} MCP servers</span>
          <Cpu size={12} />
        </div>
      </div>
    </div>
  );
}
