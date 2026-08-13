import React, { useState } from 'react';
import { SessionInfo as ProtoSessionInfo, SessionStatus } from '../gen/localharness/v1/localharness_pb';
import { LayoutGrid, List, Search, SlidersHorizontal, Clock, Archive, Plus, Loader, AlertCircle, CheckCircle, Share2 } from 'lucide-react';

interface SessionsManagerProps {
  sessions: ProtoSessionInfo[];
}

export function SessionsManager({ sessions }: SessionsManagerProps) {
  const [viewType, setViewType] = useState<'board' | 'list'>('board');
  const [searchQuery, setSearchQuery] = useState('');

  // Group sessions by status for the Board view
  const runningSessions = sessions.filter(s => s.status === SessionStatus.RUNNING);
  const blockedSessions = sessions.filter(s => s.status === SessionStatus.BLOCKED);
  const readySessions = sessions.filter(s => s.status === SessionStatus.READY || s.status === SessionStatus.UNSPECIFIED || s.status === SessionStatus.ERROR);

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
    <div className="flex-1 flex flex-col h-full bg-[#0A0A0A] overflow-hidden text-sm">
      {/* Top Bar */}
      <div className="flex items-center justify-between p-4 border-b border-[#262626]">
        <div className="flex bg-[#121212] border border-[#262626] rounded-md p-0.5">
          <button 
            onClick={() => setViewType('board')}
            className={`px-3 py-1 rounded text-xs font-medium flex items-center gap-1.5 transition-colors ${viewType === 'board' ? 'bg-[#262626] text-white' : 'text-[#9CA3AF] hover:text-white'}`}
          >
            <LayoutGrid size={14} /> Board
          </button>
          <button 
            onClick={() => setViewType('list')}
            className={`px-3 py-1 rounded text-xs font-medium flex items-center gap-1.5 transition-colors ${viewType === 'list' ? 'bg-[#262626] text-white' : 'text-[#9CA3AF] hover:text-white'}`}
          >
            <List size={14} /> List
          </button>
        </div>

        <div className="flex items-center gap-3">
          <div className="relative">
            <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-[#9CA3AF]" />
            <input 
              type="text" 
              placeholder="Search sessions..." 
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="bg-[#121212] border border-[#262626] rounded-md pl-9 pr-4 py-1.5 text-xs text-white focus:outline-none focus:border-[#333333] w-64"
            />
          </div>
          <button className="flex items-center gap-1.5 text-xs font-medium text-[#9CA3AF] hover:text-white px-3 py-1.5 bg-[#121212] border border-[#262626] rounded-md transition-colors">
            Display <SlidersHorizontal size={14} />
          </button>
        </div>
      </div>

      {/* Filter Bar */}
      <div className="flex items-center gap-2 p-4 border-b border-[#262626]">
        <div className="flex items-center gap-1.5 px-2.5 py-1 bg-[#121212] border border-[#262626] rounded text-xs text-[#9CA3AF]">
          <Clock size={12} /> Time is <span className="text-white">Any time</span>
          <span className="cursor-pointer ml-1 text-[#6B7280] hover:text-white">×</span>
        </div>
        <div className="flex items-center gap-1.5 px-2.5 py-1 bg-[#121212] border border-[#262626] rounded text-xs text-[#9CA3AF]">
          <Archive size={12} /> Archived is <span className="text-white">Excluded</span>
          <span className="cursor-pointer ml-1 text-[#6B7280] hover:text-white">×</span>
        </div>
        <button className="flex items-center justify-center w-6 h-6 rounded-full border border-[#262626] text-[#9CA3AF] hover:bg-[#262626] hover:text-white transition-colors">
          <Plus size={14} />
        </button>
      </div>

      {/* Content Area */}
      <div className="flex-1 overflow-auto">
        {viewType === 'board' ? (
          <div className="flex h-full min-w-max">
            {/* Running Column */}
            <div className="flex-1 min-w-[300px] border-r border-[#262626] p-4 flex flex-col gap-3">
              <div className="flex items-center gap-2 mb-2">
                <Loader size={14} className="text-[#9CA3AF]" />
                <span className="text-xs font-semibold text-white">Running</span>
                <span className="text-xs text-[#6B7280]">{runningSessions.length}</span>
              </div>
              {runningSessions.map(session => (
                <div key={session.id} className="bg-[#121212] border border-[#262626] rounded-md p-3 hover:border-[#333333] cursor-pointer transition-colors">
                  <div className="text-xs font-medium text-white mb-4 line-clamp-2">{session.name}</div>
                  <div className="text-[10px] text-[#6B7280]">{formatTimeAgo(session.updatedAt)}</div>
                </div>
              ))}
            </div>

            {/* Blocked Column */}
            <div className="flex-1 min-w-[300px] border-r border-[#262626] p-4 flex flex-col gap-3">
              <div className="flex items-center gap-2 mb-2">
                <AlertCircle size={14} className="text-orange-500" />
                <span className="text-xs font-semibold text-white">Blocked</span>
                <span className="text-xs text-[#6B7280]">{blockedSessions.length}</span>
              </div>
              {blockedSessions.map(session => (
                <div key={session.id} className="bg-[#121212] border border-[#262626] rounded-md p-3 hover:border-[#333333] cursor-pointer transition-colors">
                  <div className="text-xs font-medium text-white mb-4 line-clamp-2">{session.name}</div>
                  <div className="text-[10px] text-[#6B7280]">{formatTimeAgo(session.updatedAt)}</div>
                </div>
              ))}
            </div>

            {/* Ready Column */}
            <div className="flex-1 min-w-[300px] p-4 flex flex-col gap-3">
              <div className="flex items-center gap-2 mb-2">
                <CheckCircle size={14} className="text-emerald-500" />
                <span className="text-xs font-semibold text-white">Ready</span>
                <span className="text-xs text-[#6B7280]">{readySessions.length}</span>
              </div>
              {readySessions.map(session => (
                <div key={session.id} className="bg-[#121212] border border-[#262626] rounded-md p-3 hover:border-[#333333] cursor-pointer transition-colors">
                  <div className="text-xs font-medium text-white mb-4 line-clamp-2">{session.name}</div>
                  <div className="text-[10px] text-[#6B7280]">{formatTimeAgo(session.updatedAt)}</div>
                </div>
              ))}
            </div>
          </div>
        ) : (
          <div className="flex flex-col p-4 gap-1">
            {sessions.map(session => (
              <div key={session.id} className="group flex items-center py-2 px-4 hover:bg-[#121212] rounded-md cursor-pointer transition-colors">
                <Share2 size={14} className="text-white mr-3 opacity-0 group-hover:opacity-100 transition-opacity" />
                <div className="text-xs font-semibold text-white min-w-[200px] max-w-[300px] truncate pr-4 flex items-center gap-2">
                  {session.name} <span className="text-[#9CA3AF] font-normal text-[11px]">Devin Local</span>
                </div>
                <div className="text-xs text-[#9CA3AF] flex items-center gap-1.5 flex-1 truncate pr-4">
                  {session.workspace ? `My current workspace directory is \`${session.workspace}\`` : 'Local Workspace'}
                </div>
                <div className="text-xs text-[#6B7280] whitespace-nowrap">
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
