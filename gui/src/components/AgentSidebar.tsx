import { Plus, MessageSquare, Search, Filter, Settings, Cpu, FolderOpen } from 'lucide-react';
import { useState, useEffect } from 'react';
import { SessionInfo as ProtoSessionInfo } from '../gen/localharness/v1/localharness_pb';

import { Space } from '../App';

interface AgentSidebarProps {
  activeSessionId: string | null;
  onSelectSession: (id: string) => void;
  onNewSession: () => void;
  onCreateSpace?: () => void;
  onMoveSessionToSpace?: (sessionId: string, spaceId: string) => void;
  onOpenCustomizations: () => void;
  onOpenSessionsManager: () => void;
  onDeleteSession?: (sessionId: string) => void;
  onArchiveSession?: (sessionId: string) => void;
  sessions: ProtoSessionInfo[];
  spaces?: Space[];
  sessionSpaces?: Record<string, string>;
  mcpServerCount?: number;
}

export function AgentSidebar({ 
  activeSessionId, 
  onSelectSession, 
  onNewSession, 
  onCreateSpace,
  onMoveSessionToSpace,
  onOpenCustomizations,
  onOpenSessionsManager,
  onDeleteSession,
  onArchiveSession,
  sessions,
  spaces = [],
  sessionSpaces = {},
  mcpServerCount = 0
}: AgentSidebarProps) {
  const [contextMenu, setContextMenu] = useState<{ x: number, y: number, sessionId: string } | null>(null);

  useEffect(() => {
    const handleClick = () => setContextMenu(null);
    document.addEventListener('click', handleClick);
    return () => document.removeEventListener('click', handleClick);
  }, []);

  return (
    <div className="w-full h-full bg-bg-primary border-r border-border-primary flex flex-col text-text-primary">
      {/* Top action */}
      <div className="p-3 pb-2">
        <button 
          data-testid="btn-new-session"
          onClick={onNewSession}
          className="w-full flex items-center gap-2 bg-bg-secondary hover:bg-bg-tertiary text-text-primary px-3 py-2 rounded-md text-sm font-medium transition-colors border border-border-primary"
        >
          <Plus size={16} className="text-text-secondary" /> 
          New session
        </button>
      </div>


      {/* Main Nav */}
      <div className="px-2">
        <div 
          data-testid="btn-sessions-manager"
          className="flex items-center gap-2 px-3 py-1.5 text-xs font-semibold text-text-primary bg-bg-secondary cursor-pointer hover:bg-bg-tertiary transition-colors rounded-sm"
          onClick={onOpenSessionsManager}
        >
          <MessageSquare size={14} />
          <span>Sessions</span>
        </div>
      </div>

      {/* Spaces Header */}
      <div className="px-4 mt-6 mb-2 flex items-center justify-between text-xs font-semibold text-text-secondary">
        <span>Spaces</span>
        <div className="flex items-center gap-2">
          <Search size={14} className="cursor-pointer hover:text-text-primary" />
          <Plus data-testid="btn-create-space" size={14} className="cursor-pointer hover:text-text-primary" onClick={onCreateSpace} />
          <Filter size={14} className="cursor-pointer hover:text-text-primary" />
        </div>
      </div>

      {/* Sessions List */}
      <div className="flex-1 overflow-y-auto px-2 pb-4">
        {(() => {
          if (sessions.length === 0) {
            return (
              <div className="px-2 py-4 text-xs text-text-tertiary">
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
              <div className="px-2 py-1 flex items-center gap-1.5 text-[11px] font-semibold tracking-wide text-text-tertiary uppercase">
                <FolderOpen size={12} />
                <span className="truncate" title={spaceName}>{spaceName}</span>
              </div>
              <div className="space-y-0.5 mt-1">
                {spaceSessions.map((session: ProtoSessionInfo) => (
                  <div 
                    key={session.id}
                    data-testid="session-item"
                    onClick={() => onSelectSession(session.id)}
                    onContextMenu={(e) => {
                      e.preventDefault();
                      setContextMenu({ x: e.clientX, y: e.clientY, sessionId: session.id });
                    }}
                    className={`w-full text-left px-2 py-1.5 rounded-md cursor-pointer transition-colors ${
                      activeSessionId === session.id 
                        ? 'bg-border-primary text-text-primary' 
                        : 'text-text-secondary hover:text-text-primary hover:bg-bg-secondary'
                    }`}
                  >
                    <div className="text-sm font-medium truncate">{session.name || "Untitled session"}</div>
                    <div className="text-[11px] text-text-tertiary truncate mt-0.5">
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
          className="fixed z-50 bg-bg-secondary border border-border-primary rounded-md shadow-lg py-1 min-w-[150px] text-xs"
          style={{ top: contextMenu.y, left: contextMenu.x }}
        >
          <div className="px-3 py-1.5 text-text-tertiary font-semibold">Move to Space</div>
          {spaces.map(space => (
            <button
              key={space.id}
              className="w-full text-left px-3 py-1.5 text-text-primary hover:bg-bg-tertiary"
              onClick={() => onMoveSessionToSpace?.(contextMenu.sessionId, space.id)}
            >
              {space.name}
            </button>
          ))}
          {spaces.length === 0 && (
            <div className="px-3 py-1.5 text-text-tertiary italic">No spaces created</div>
          )}
          <div className="border-t border-border-primary my-1"></div>
          <button
            className="w-full text-left px-3 py-1.5 text-text-primary hover:bg-bg-tertiary transition-colors"
            onClick={() => onArchiveSession?.(contextMenu.sessionId)}
          >
            Archive Session
          </button>
          <button
            className="w-full text-left px-3 py-1.5 text-red-500 hover:bg-bg-tertiary hover:text-red-400"
            onClick={() => onDeleteSession?.(contextMenu.sessionId)}
          >
            Delete Session
          </button>
        </div>
      )}

      {/* Footer */}
      <div className="p-3 border-t border-border-primary flex flex-col gap-2">
        <div className="flex gap-2">
          <button 
            data-testid="btn-customizations"
            onClick={onOpenCustomizations}
            className="flex flex-1 items-center justify-between px-2 py-1.5 text-xs font-semibold text-text-primary hover:bg-bg-secondary rounded-md transition-colors"
          >
            <span>Customizations</span>
            <Settings size={14} className="text-text-secondary" />
          </button>
        </div>
        <div className="flex items-center justify-between px-2 text-[11px] text-text-tertiary font-medium">
          <span>{mcpServerCount} MCP servers</span>
          <Cpu size={12} />
        </div>
      </div>
    </div>
  );
}
