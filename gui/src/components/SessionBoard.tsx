import { Search, Filter, Clock, Play, AlertCircle } from 'lucide-react';
import { SessionInfo, SessionStatus } from '../gen/localharness/v1/localharness_pb';

interface SessionBoardProps {
  sessions: SessionInfo[];
  onSelectSession: (id: string) => void;
}

export function SessionBoard({ sessions, onSelectSession }: SessionBoardProps) {
  const runningSessions = sessions.filter(s => s.status === SessionStatus.RUNNING);
  const blockedSessions = sessions.filter(s => s.status === SessionStatus.BLOCKED);
  const readySessions = sessions.filter(s => s.status === SessionStatus.READY || s.status === SessionStatus.UNSPECIFIED);

  const renderColumn = (title: string, count: number, sessionsList: SessionInfo[], icon: React.ReactNode, colorClass: string) => (
    <div className="flex-1 flex flex-col min-w-[300px]">
      <div className="flex items-center gap-2 mb-4 px-2">
        <span className={colorClass}>{icon}</span>
        <span className="font-semibold text-sm text-[#cdd6f4]">{title}</span>
        <span className="text-xs text-[#6c7086] font-medium">{count}</span>
      </div>
      <div className="flex-1 flex flex-col gap-3 overflow-y-auto px-2 pb-6">
        {sessionsList.map(session => (
          <div 
            key={session.id}
            onClick={() => onSelectSession(session.id)}
            className="bg-[#1e1e2e] border border-[#313244] hover:border-[#45475a] p-4 rounded-lg cursor-pointer transition-all shadow-sm flex flex-col gap-3"
          >
            <div className="text-[13px] font-medium text-[#cdd6f4] leading-relaxed">
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
          <div className="border border-dashed border-[#313244] rounded-lg p-6 flex items-center justify-center text-xs text-[#585b70]">
            No sessions
          </div>
        )}
      </div>
    </div>
  );

  return (
    <div className="flex-1 bg-[#11111b] text-[#cdd6f4] flex flex-col h-full overflow-hidden">
      {/* Top Bar */}
      <div className="p-4 border-b border-[#181825] flex items-center justify-between">
        <div className="flex items-center gap-4">
          <div className="flex bg-[#181825] rounded p-1 text-sm font-medium">
            <button className="px-3 py-1 bg-[#313244] text-[#cdd6f4] rounded shadow-sm">Board</button>
            <button className="px-3 py-1 text-[#6c7086] hover:text-[#cdd6f4]">List</button>
          </div>
          <div className="flex items-center gap-2">
            <div className="flex items-center gap-1 bg-[#181825] px-2 py-1 rounded text-xs border border-[#313244]">
              <Clock size={12} className="text-[#6c7086]" />
              <span className="text-[#a6adc8]">Time is <span className="text-[#cdd6f4]">Any time</span></span>
            </div>
            <div className="flex items-center gap-1 bg-[#181825] px-2 py-1 rounded text-xs border border-[#313244]">
              <Filter size={12} className="text-[#6c7086]" />
              <span className="text-[#a6adc8]">Archived is <span className="text-[#cdd6f4]">Excluded</span></span>
            </div>
            <button className="p-1 hover:bg-[#181825] rounded border border-transparent hover:border-[#313244] text-[#a6adc8]">
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
              className="bg-[#181825] text-sm py-1.5 pl-8 pr-4 rounded border border-[#313244] focus:outline-none focus:border-[#89b4fa] w-[250px]"
            />
          </div>
          <div className="flex items-center gap-2 text-sm font-medium">
            <span className="text-[#6c7086]">Display</span>
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
