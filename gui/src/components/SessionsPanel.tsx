import { MoreHorizontal, Plus, Search, Filter, Clock } from 'lucide-react';
import { SessionInfo as ProtoSessionInfo } from '../gen/localharness/v1/localharness_pb';

interface SessionsPanelProps {
  activeSessionId: string | null;
  onSelectSession: (id: string) => void;
  onNewSession: () => void;
  sessions: ProtoSessionInfo[];
}

export function SessionsPanel({ activeSessionId, onSelectSession, onNewSession, sessions }: SessionsPanelProps) {

  return (
    <div className="h-full bg-bg-primary border-r border-border-primary flex flex-col text-text-primary">
      {/* Header section */}
      <div className="p-4 flex items-center justify-between border-b border-border-primary">
        <div className="flex items-center gap-2">
          <div className="w-4 h-4 bg-accent-primary rounded-sm opacity-80" style={{ backgroundColor: 'var(--accent-primary)' }} />
          <span className="font-semibold text-[13px] tracking-wide text-accent-primary" style={{ color: 'var(--accent-primary)' }}>Sessions</span>
        </div>
        <div className="flex items-center gap-1">
          <button className="p-1 hover:bg-bg-tertiary rounded text-text-tertiary transition-colors">
            <MoreHorizontal size={16} />
          </button>
          <button onClick={onNewSession} className="flex items-center gap-1 bg-accent-primary hover:bg-accent-secondary text-bg-primary px-2 py-1 rounded text-[13px] font-medium transition-colors" style={{ backgroundColor: 'var(--accent-primary)', color: 'var(--bg-primary)' }}>
            <Plus size={14} /> New
          </button>
        </div>
      </div>

      {/* Search section */}
      <div className="p-3 border-b border-border-primary">
        <div className="relative flex items-center">
          <Search size={14} className="absolute left-2.5 text-text-tertiary" />
          <input
            type="text"
            placeholder="Search sessions..."
            className="w-full bg-bg-secondary text-text-primary placeholder:text-text-tertiary text-[13px] py-1.5 pl-8 pr-8 rounded border border-border-primary focus:outline-none focus:border-accent-primary transition-colors"
            style={{ '--tw-focus-ring-color': 'var(--accent-primary)' } as any}
          />
          <Filter size={14} className="absolute right-2.5 text-text-tertiary cursor-pointer hover:text-text-primary" />
        </div>
      </div>

      {/* Sessions list */}
      <div className="flex-1 overflow-y-auto">
        {sessions.length === 0 ? (
          <div className="p-4 text-center text-xs text-text-tertiary opacity-70">
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
                    ? 'bg-bg-tertiary/40 border border-border-primary'
                    : 'hover:bg-bg-secondary border border-transparent hover:border-border-primary'
                }`}
              >
                <div className="text-[10px] uppercase font-bold text-error mb-1" style={{ color: 'var(--error)' }}>LOCAL-HARNESS</div>
                <div className="text-[13px] font-medium text-text-primary truncate mb-1">
                  {session.name}
                </div>
                <div className="text-[11px] text-text-tertiary flex items-center justify-between">
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
