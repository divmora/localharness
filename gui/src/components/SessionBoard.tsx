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
        <span className="font-semibold text-sm text-text-primary">{title}</span>
        <span className="text-xs text-text-tertiary font-medium">{count}</span>
      </div>
      <div className="flex-1 flex flex-col gap-3 overflow-y-auto px-2 pb-6">
        {sessionsList.map(session => (
          <div
            key={session.id}
            onClick={() => onSelectSession(session.id)}
            className="bg-bg-tertiary border border-border-primary hover:border-border-highlight p-4 rounded-lg cursor-pointer transition-all shadow-sm flex flex-col gap-3"
          >
            <div className="text-[13px] font-medium text-text-primary leading-relaxed">
              {session.name}
            </div>
            <div className="flex items-center justify-between text-[11px] text-text-tertiary font-medium">
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
          <div className="border border-dashed border-border-primary rounded-lg p-6 flex items-center justify-center text-xs text-text-tertiary">
            No sessions
          </div>
        )}
      </div>
    </div>
  );

  return (
    <div className="flex-1 bg-bg-primary text-text-primary flex flex-col h-full overflow-hidden">
      {/* Top Bar */}
      <div className="p-4 border-b border-border-primary flex items-center justify-between">
        <div className="flex items-center gap-4">
          <div className="flex bg-bg-secondary rounded p-1 text-sm font-medium">
            <button className="px-3 py-1 bg-bg-tertiary text-text-primary rounded shadow-sm">Board</button>
            <button className="px-3 py-1 text-text-tertiary hover:text-text-primary">List</button>
          </div>
          <div className="flex items-center gap-2">
            <div className="flex items-center gap-1 bg-bg-secondary px-2 py-1 rounded text-xs border border-border-primary">
              <Clock size={12} className="text-text-tertiary" />
              <span className="text-text-tertiary">Time is <span className="text-text-primary">Any time</span></span>
            </div>
            <div className="flex items-center gap-1 bg-bg-secondary px-2 py-1 rounded text-xs border border-border-primary">
              <Filter size={12} className="text-text-tertiary" />
              <span className="text-text-tertiary">Archived is <span className="text-text-primary">Excluded</span></span>
            </div>
            <button className="p-1 hover:bg-bg-secondary rounded border border-transparent hover:border-border-primary text-text-tertiary">
              <Filter size={14} />
            </button>
          </div>
        </div>
        <div className="flex items-center gap-4">
          <div className="relative">
            <Search size={14} className="absolute left-2.5 top-2 text-text-tertiary" />
            <input
              type="text"
              placeholder="Search sessions..."
              className="bg-bg-secondary text-sm py-1.5 pl-8 pr-4 rounded border border-border-primary focus:outline-none focus:border-accent-primary w-[250px]"
              style={{ '--tw-focus-ring-color': 'var(--accent-primary)' } as any}
            />
          </div>
          <div className="flex items-center gap-2 text-sm font-medium">
            <span className="text-text-tertiary">Display</span>
            <button onClick={onNewSession} className="flex items-center gap-1 bg-accent-primary hover:bg-accent-secondary text-bg-primary px-3 py-1.5 rounded text-xs font-semibold transition-colors" style={{ backgroundColor: 'var(--accent-primary)', color: 'var(--bg-primary)' }}>
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
