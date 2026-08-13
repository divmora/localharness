import { Search, Filter, Clock, Play, AlertCircle, Plus } from 'lucide-react';
import { SessionInfo, SessionStatus } from '../gen/localharness/v1/localharness_pb';

interface SessionBoardProps {
  sessions: SessionInfo[];
  onSelectSession: (id: string) => void;
  onNewSession: () => void;
}

export function SessionBoard({ sessions, onSelectSession, onNewSession }: SessionBoardProps) {
  const runningSessions = sessions.filter(s => s.status === SessionStatus.RUNNING);
  const blockedSessions = sessions.filter(s => s.status === SessionStatus.BLOCKED);
  const readySessions = sessions.filter(s => s.status === SessionStatus.READY || s.status === SessionStatus.UNSPECIFIED);

  const renderColumn = (title: string, count: number, sessionsList: SessionInfo[], icon: React.ReactNode, colorClass: string) => (
    <div className="flex-1 flex flex-col min-w-[300px]">
      <div className="flex items-center gap-2 mb-4 px-2">
        <span className={colorClass}>{icon}</span>
        <span className="font-semibold text-sm text-[#F9FAFB]">{title}</span>
        <span className="text-xs text-[#6c7086] font-medium">{count}</span>
      </div>
      <div className="flex-1 flex flex-col gap-3 overflow-y-auto px-2 pb-6">
        {sessionsList.map(session => (
          <div 
            key={session.id}
            onClick={() => onSelectSession(session.id)}
            className="bg-[#121212] border border-[#262626] hover:border-[#333333] p-4 rounded-lg cursor-pointer transition-all shadow-sm flex flex-col gap-3"
          >
            <div className="text-[13px] font-medium text-[#F9FAFB] leading-relaxed">
              {session.name}
            </div>
            <div className="flex items-center justify-between text-[11px] text-[#6c7086] font-medium">
              <span className="flex items-center gap-1">
                <Clock size={12} />
                {new Date(Number(session.updatedAt) * 1000).toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'})}
              </span>
              <span className="flex items-center gap-1">
                {session.id.substring(0, 8)}
              </span>
            </div>
          </div>
        ))}
        {sessionsList.length === 0 && (
          <div className="border border-dashed border-[#262626] rounded-lg p-6 flex items-center justify-center text-xs text-[#585b70]">
            No sessions
          </div>
        )}
      </div>
    </div>
  );

  return (
    <div className="flex-1 bg-[#000000] text-[#F9FAFB] flex flex-col h-full overflow-hidden">
      {/* Top Bar */}
      <div className="p-4 border-b border-[#0A0A0A] flex items-center justify-between">
        <div className="flex items-center gap-4">
          <div className="flex bg-[#0A0A0A] rounded p-1 text-sm font-medium">
            <button className="px-3 py-1 bg-[#262626] text-[#F9FAFB] rounded shadow-sm">Board</button>
            <button className="px-3 py-1 text-[#6c7086] hover:text-[#F9FAFB]">List</button>
          </div>
          <div className="flex items-center gap-2">
            <div className="flex items-center gap-1 bg-[#0A0A0A] px-2 py-1 rounded text-xs border border-[#262626]">
              <Clock size={12} className="text-[#6c7086]" />
              <span className="text-[#9CA3AF]">Time is <span className="text-[#F9FAFB]">Any time</span></span>
            </div>
            <div className="flex items-center gap-1 bg-[#0A0A0A] px-2 py-1 rounded text-xs border border-[#262626]">
              <Filter size={12} className="text-[#6c7086]" />
              <span className="text-[#9CA3AF]">Archived is <span className="text-[#F9FAFB]">Excluded</span></span>
            </div>
            <button className="p-1 hover:bg-[#0A0A0A] rounded border border-transparent hover:border-[#262626] text-[#9CA3AF]">
              <Filter size={14} />
            </button>
          </div>
        </div>
        <div className="flex items-center gap-4">
          <div className="relative">
            <Search size={14} className="absolute left-2.5 top-2 text-[#6c7086]" />
            <input 
              type="text" 
              placeholder="Search sessions..." 
              className="bg-[#0A0A0A] text-sm py-1.5 pl-8 pr-4 rounded border border-[#262626] focus:outline-none focus:border-[#3B82F6] w-[250px]"
            />
          </div>
          <div className="flex items-center gap-2 text-sm font-medium">
            <span className="text-[#6c7086]">Display</span>
            <button onClick={onNewSession} className="flex items-center gap-1 bg-[#3B82F6] hover:bg-[#60A5FA] text-[#000000] px-3 py-1.5 rounded text-xs font-semibold transition-colors">
              <Plus size={14} /> New Session
            </button>
          </div>
        </div>
      </div>

      {/* Board Columns */}
      <div className="flex-1 p-6 flex gap-6 overflow-x-auto">
        {renderColumn("Running", runningSessions.length, runningSessions, <Play size={14} fill="currentColor" />, "text-blue-400")}
        {renderColumn("Blocked", blockedSessions.length, blockedSessions, <AlertCircle size={14} />, "text-orange-400")}
        {renderColumn("Ready", readySessions.length, readySessions, <div className="w-3 h-3 rounded-full border-2 border-green-500 flex items-center justify-center"><div className="w-1.5 h-1.5 bg-green-500 rounded-full"></div></div>, "")}
      </div>
    </div>
  );
}
