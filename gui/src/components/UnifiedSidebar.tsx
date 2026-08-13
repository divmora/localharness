import { Plus, MessageSquare, Search, Filter, Settings, Cpu } from 'lucide-react';
import { SessionInfo as ProtoSessionInfo } from '../gen/localharness/v1/localharness_pb';

interface UnifiedSidebarProps {
  activeSessionId: string | null;
  onSelectSession: (id: string) => void;
  onNewSession: () => void;
  onOpenCustomizations: () => void;
  sessions: ProtoSessionInfo[];
  mcpServerCount?: number;
}

export function UnifiedSidebar({ 
  activeSessionId, 
  onSelectSession, 
  onNewSession, 
  onOpenCustomizations,
  sessions,
  mcpServerCount = 0
}: UnifiedSidebarProps) {

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
        <button className="w-full flex items-center gap-2 px-2 py-1.5 text-sm font-medium text-[#F9FAFB] hover:bg-[#0A0A0A] rounded-md transition-colors">
          <MessageSquare size={16} className="text-[#9CA3AF]" />
          Sessions
        </button>
      </div>

      {/* Spaces Header */}
      <div className="px-4 mt-6 mb-2 flex items-center justify-between text-xs font-semibold text-[#9CA3AF]">
        <span>Spaces</span>
        <div className="flex items-center gap-2">
          <Search size={14} className="cursor-pointer hover:text-[#F9FAFB]" />
          <Plus size={14} className="cursor-pointer hover:text-[#F9FAFB]" />
          <Filter size={14} className="cursor-pointer hover:text-[#F9FAFB]" />
        </div>
      </div>

      {/* Sessions List */}
      <div className="flex-1 overflow-y-auto px-2 space-y-0.5">
        {sessions.map((session) => (
          <div 
            key={session.id}
            onClick={() => onSelectSession(session.id)}
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
        {sessions.length === 0 && (
          <div className="px-2 py-4 text-xs text-[#6B7280]">
            No recent sessions
          </div>
        )}
      </div>

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
