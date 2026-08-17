import { useState, useMemo, useEffect } from 'react';
import { SessionInfo as ProtoSessionInfo, SessionStatus } from '../gen/localharness/v1/localharness_pb';
import { LayoutGrid, List, Search, SlidersHorizontal, Clock, Archive, Plus, Loader, AlertCircle, CheckCircle, Share2, Trash2 } from 'lucide-react';
import { SkeletonCard } from './SkeletonLoader';

interface SessionsManagerProps {
  sessions: ProtoSessionInfo[];
  onSelectSession: (id: string) => void;
  onDeleteSession?: (id: string) => void;
}

export function SessionsManager({ sessions, onSelectSession, onDeleteSession }: SessionsManagerProps) {
  const [viewType, setViewType] = useState<'board' | 'list'>('board');
  const [searchQuery, setSearchQuery] = useState('');
  const [isLoading, setIsLoading] = useState(false);

  // Simulate loading state
  useEffect(() => {
    setIsLoading(true);
    const timer = setTimeout(() => setIsLoading(false), 500);
    return () => clearTimeout(timer);
  }, [sessions]);

  const filteredSessions = useMemo(() => {
    return sessions.filter(session => {
      if (searchQuery && !session.name.toLowerCase().includes(searchQuery.toLowerCase())) {
        return false;
      }
      return true;
    });
  }, [sessions, searchQuery]);

  // Group sessions by status for the Board view
  const runningSessions = filteredSessions.filter(s => s.status === SessionStatus.RUNNING);
  const blockedSessions = filteredSessions.filter(s => s.status === SessionStatus.BLOCKED);
  const readySessions = filteredSessions.filter(s => s.status === SessionStatus.READY || s.status === SessionStatus.UNSPECIFIED || s.status === SessionStatus.ERROR);

  // Time formatter
  const formatTimeAgo = (updatedAt: bigint) => {
    const diff = Date.now() - Number(updatedAt) * 1000;
    const hours = Math.floor(diff / (1000 * 60 * 60));
    if (hours === 0) {
      const mins = Math.floor(diff / (1000 * 60));
      return `${mins}m ago`;
    }
    return `${hours}h ago`;
  };

  return (
    <div className="flex-1 flex flex-col h-full bg-bg-secondary overflow-hidden text-sm">
      {/* Top Bar */}
      <div className="flex items-center justify-between p-4 border-b border-border-primary">
        <div className="flex bg-bg-tertiary border border-border-primary rounded-md p-0.5">
          <button 
            onClick={() => setViewType('board')}
            className={`px-3 py-1 rounded text-xs font-medium flex items-center gap-1.5 transition-colors ${viewType === 'board' ? 'bg-border-primary text-text-primary' : 'text-text-secondary hover:text-text-primary'}`}
          >
            <LayoutGrid size={14} /> Board
          </button>
          <button 
            onClick={() => setViewType('list')}
            className={`px-3 py-1 rounded text-xs font-medium flex items-center gap-1.5 transition-colors ${viewType === 'list' ? 'bg-border-primary text-text-primary' : 'text-text-secondary hover:text-text-primary'}`}
          >
            <List size={14} /> List
          </button>
        </div>

        <div className="flex items-center gap-3">
          <div className="relative">
            <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-text-secondary" />
            <input 
              type="text" 
              placeholder="Search sessions..." 
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="bg-bg-tertiary border border-border-primary rounded-md pl-9 pr-4 py-1.5 text-xs text-text-primary focus:outline-none focus:border-border-highlight w-64"
            />
          </div>
          <button className="flex items-center gap-1.5 text-xs font-medium text-text-secondary hover:text-text-primary px-3 py-1.5 bg-bg-tertiary border border-border-primary rounded-md transition-colors">
            Display <SlidersHorizontal size={14} />
          </button>
        </div>
      </div>

      {/* Filter Bar */}
      <div className="flex items-center gap-2 p-4 border-b border-border-primary">
        <div className="flex items-center gap-1.5 px-2.5 py-1 bg-bg-tertiary border border-border-primary rounded text-xs text-text-secondary">
          <Clock size={12} /> Time is <span className="text-text-primary">Any time</span>
          <span className="cursor-pointer ml-1 text-text-tertiary hover:text-text-primary">×</span>
        </div>
        <div className="flex items-center gap-1.5 px-2.5 py-1 bg-bg-tertiary border border-border-primary rounded text-xs text-text-secondary">
          <Archive size={12} /> Archived is <span className="text-text-primary">Excluded</span>
          <span className="cursor-pointer ml-1 text-text-tertiary hover:text-text-primary">×</span>
        </div>
        <button className="flex items-center justify-center w-6 h-6 rounded-full border border-border-primary text-text-secondary hover:bg-border-primary hover:text-text-primary transition-colors">
          <Plus size={14} />
        </button>
      </div>

      {/* Content Area */}
      <div className="flex-1 overflow-auto">
        {isLoading ? (
          <div className="p-4">
            {viewType === 'board' ? (
              <div className="flex gap-4">
                {[1, 2, 3].map((i) => (
                  <div key={i} className="flex-1 min-w-[300px] flex flex-col gap-3">
                    <SkeletonCard lines={2} />
                    <SkeletonCard lines={2} />
                    <SkeletonCard lines={2} />
                  </div>
                ))}
              </div>
            ) : (
              <div className="flex flex-col gap-2">
                {[1, 2, 3, 4, 5].map((i) => (
                  <SkeletonCard key={i} lines={1} />
                ))}
              </div>
            )}
          </div>
        ) : viewType === 'board' ? (
          <div className="flex h-full min-w-max">
            {/* Running Column */}
            <div className="flex-1 min-w-[300px] border-r border-border-primary p-4 flex flex-col gap-3">
              <div className="flex items-center gap-2 mb-2">
                <Loader size={14} className="text-text-secondary" />
                <span className="text-xs font-semibold text-text-primary">Running</span>
                <span className="text-xs text-text-tertiary">{runningSessions.length}</span>
              </div>
              {runningSessions.map(session => (
                <div key={session.id} onClick={() => onSelectSession(session.id)} className="bg-bg-tertiary border border-border-primary rounded-md p-3 hover:border-border-highlight cursor-pointer transition-colors">
                  <div className="text-xs font-medium text-text-primary mb-4 line-clamp-2">{session.name}</div>
                  <div className="text-[10px] text-text-tertiary">{formatTimeAgo(session.updatedAt)}</div>
                </div>
              ))}
            </div>

            {/* Blocked Column */}
            <div className="flex-1 min-w-[300px] border-r border-border-primary p-4 flex flex-col gap-3">
              <div className="flex items-center gap-2 mb-2">
                <AlertCircle size={14} className="text-orange-500" />
                <span className="text-xs font-semibold text-text-primary">Blocked</span>
                <span className="text-xs text-text-tertiary">{blockedSessions.length}</span>
              </div>
              {blockedSessions.map(session => (
                <div key={session.id} onClick={() => onSelectSession(session.id)} className="bg-bg-tertiary border border-border-primary rounded-md p-3 hover:border-border-highlight cursor-pointer transition-colors">
                  <div className="text-xs font-medium text-text-primary mb-4 line-clamp-2">{session.name}</div>
                  <div className="text-[10px] text-text-tertiary">{formatTimeAgo(session.updatedAt)}</div>
                </div>
              ))}
            </div>

            {/* Ready Column */}
            <div className="flex-1 min-w-[300px] p-4 flex flex-col gap-3">
              <div className="flex items-center gap-2 mb-2">
                <CheckCircle size={14} className="text-emerald-500" />
                <span className="text-xs font-semibold text-text-primary">Ready</span>
                <span className="text-xs text-text-tertiary">{readySessions.length}</span>
              </div>
              {readySessions.map(session => (
                <div key={session.id} onClick={() => onSelectSession(session.id)} className="bg-bg-tertiary border border-border-primary rounded-md p-3 hover:border-border-highlight cursor-pointer transition-colors">
                  <div className="text-xs font-medium text-text-primary mb-4 line-clamp-2">{session.name}</div>
                  <div className="text-[10px] text-text-tertiary">{formatTimeAgo(session.updatedAt)}</div>
                </div>
              ))}
            </div>
          </div>
        ) : (
          <div className="flex flex-col p-4 gap-1">
            {filteredSessions.length === 0 && (
              <div className="text-text-tertiary text-xs text-center py-8">No sessions found matching your criteria.</div>
            )}
            {filteredSessions.map(session => (
              <div key={session.id} onClick={() => onSelectSession(session.id)} className="group flex items-center py-2 px-4 hover:bg-bg-tertiary rounded-md cursor-pointer transition-colors">
                <Share2 size={14} className="text-text-primary mr-3 opacity-0 group-hover:opacity-100 transition-opacity" />
                <div className="text-xs font-semibold text-text-primary min-w-[200px] max-w-[300px] truncate pr-4 flex items-center gap-2">
                  {session.name} <span className="text-text-secondary font-normal text-[11px]">Devin Local</span>
                </div>
                <div className="text-xs text-text-secondary flex items-center gap-1.5 flex-1 truncate pr-4">
                  {session.workspace ? `My current workspace directory is \`${session.workspace}\`` : 'Local Workspace'}
                  <button 
                    className="p-1.5 text-text-tertiary hover:text-red-500 hover:bg-bg-tertiary rounded transition-colors"
                    title="Delete Session"
                    onClick={(e) => {
                      e.stopPropagation();
                      if (onDeleteSession) onDeleteSession(session.id);
                    }}
                  >
                    <Trash2 size={16} />
                  </button>
                </div>
                <div className="text-xs text-text-tertiary whitespace-nowrap">
                  {formatTimeAgo(session.updatedAt)}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
