import { MoreHorizontal, Plus, Search, Filter, Clock } from 'lucide-react';
import { SessionInfo as ProtoSessionInfo } from '../gen/localharness/v1/localharness_pb';

interface SessionsPanelProps {
  activeSessionId: string | null;
  onSelectSession: (id: string) => void;
  sessions: ProtoSessionInfo[];
}

export function SessionsPanel({ activeSessionId, onSelectSession, sessions }: SessionsPanelProps) {

  return (
    <div className="h-full bg-[#000000] border-r border-[#0A0A0A] flex flex-col text-[#F9FAFB]">
      {/* Header section */}
      <div className="p-4 flex items-center justify-between border-b border-[#0A0A0A]">
        <div className="flex items-center gap-2">
          <div className="w-4 h-4 bg-[#60A5FA] rounded-sm opacity-80" />
          <span className="font-semibold text-[13px] tracking-wide text-[#60A5FA]">Sessions</span>
        </div>
        <div className="flex items-center gap-1">
          <button className="p-1 hover:bg-[#262626] rounded text-[#9CA3AF] transition-colors">
            <MoreHorizontal size={16} />
          </button>
          <button className="flex items-center gap-1 bg-[#3B82F6] hover:bg-[#60A5FA] text-[#000000] px-2 py-1 rounded text-[13px] font-medium transition-colors">
            <Plus size={14} /> New
          </button>
        </div>
      </div>

      {/* Search section */}
      <div className="p-3 border-b border-[#0A0A0A]">
        <div className="relative flex items-center">
          <Search size={14} className="absolute left-2.5 text-[#6c7086]" />
          <input 
            type="text" 
            placeholder="Search sessions..." 
            className="w-full bg-[#0A0A0A] text-[#F9FAFB] placeholder-[#6c7086] text-[13px] py-1.5 pl-8 pr-8 rounded border border-[#262626] focus:outline-none focus:border-[#3B82F6] transition-colors"
          />
          <Filter size={14} className="absolute right-2.5 text-[#6c7086] cursor-pointer hover:text-[#F9FAFB]" />
        </div>
      </div>

      {/* Sessions list */}
      <div className="flex-1 overflow-y-auto">
        {sessions.length === 0 ? (
          <div className="p-4 text-center text-xs text-[#9CA3AF] opacity-70">
            No active sessions found.
          </div>
        ) : (
          <div className="space-y-1 p-2">
            {sessions.map((session) => (
              <div 
                key={session.id}
                onClick={() => onSelectSession(session.id)}
                className={`p-3 rounded-lg cursor-pointer transition-all group ${
                  activeSessionId === session.id 
                    ? 'bg-[#262626]/40 border border-[#333333]' 
                    : 'hover:bg-[#0A0A0A] border border-transparent hover:border-[#262626]'
                }`}
              >
                <div className="text-[10px] uppercase font-bold text-[#EF4444] mb-1">LOCAL-HARNESS</div>
                <div className="text-[13px] font-medium text-[#F9FAFB] truncate mb-1">
                  {session.name}
                </div>
                <div className="text-[11px] text-[#6c7086] flex items-center justify-between">
                  <span className="truncate">{session.id.substring(0, 8)}...</span>
                  <span className="flex items-center gap-1">
                    <Clock size={10} />
                    {new Date(Number(session.updatedAt) * 1000).toLocaleDateString()}
                  </span>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
