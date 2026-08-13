import { MoreHorizontal, Plus, Search, Filter, Clock } from 'lucide-react';
import { useState, useEffect } from 'react';
import { invoke } from '@tauri-apps/api/core';
import { fromBinary } from '@bufbuild/protobuf';
import { SessionListSchema, SessionInfo as ProtoSessionInfo } from '../gen/localharness/v1/localharness_pb';

interface SessionsPanelProps {
  activeSessionId: string | null;
  onSelectSession: (id: string) => void;
}

export function SessionsPanel({ activeSessionId, onSelectSession }: SessionsPanelProps) {
  const [sessions, setSessions] = useState<ProtoSessionInfo[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function loadSessions() {
      try {
        // Now returns protobuf bytes (number[])
        const result = await invoke<number[]>('list_sessions');
        const sessionList = fromBinary(SessionListSchema, new Uint8Array(result));
        setSessions(sessionList.sessions);
      } catch (err) {
        console.error("Failed to list sessions:", err);
      } finally {
        setLoading(false);
      }
    }
    loadSessions();
  }, []);

  return (
    <div className="h-full bg-[#11111b] border-r border-[#181825] flex flex-col text-[#cdd6f4]">
      {/* Header section */}
      <div className="p-4 flex items-center justify-between border-b border-[#181825]">
        <div className="flex items-center gap-2">
          <div className="w-4 h-4 bg-[#b4befe] rounded-sm opacity-80" />
          <span className="font-semibold text-[13px] tracking-wide text-[#b4befe]">Sessions</span>
        </div>
        <div className="flex items-center gap-1">
          <button className="p-1 hover:bg-[#313244] rounded text-[#a6adc8] transition-colors">
            <MoreHorizontal size={16} />
          </button>
          <button className="flex items-center gap-1 bg-[#89b4fa] hover:bg-[#b4befe] text-[#11111b] px-2 py-1 rounded text-[13px] font-medium transition-colors">
            <Plus size={14} /> New
          </button>
        </div>
      </div>

      {/* Search section */}
      <div className="p-3 border-b border-[#181825]">
        <div className="relative flex items-center">
          <Search size={14} className="absolute left-2.5 text-[#6c7086]" />
          <input 
            type="text" 
            placeholder="Search sessions..." 
            className="w-full bg-[#181825] text-[#cdd6f4] placeholder-[#6c7086] text-[13px] py-1.5 pl-8 pr-8 rounded border border-[#313244] focus:outline-none focus:border-[#89b4fa] transition-colors"
          />
          <Filter size={14} className="absolute right-2.5 text-[#6c7086] cursor-pointer hover:text-[#cdd6f4]" />
        </div>
      </div>

      {/* Sessions list */}
      <div className="flex-1 overflow-y-auto p-2">
        {loading ? (
          <div className="text-xs text-[#6c7086] px-2 py-4">Loading sessions...</div>
        ) : sessions.length === 0 ? (
          <div className="text-xs text-[#6c7086] px-2 py-4">No sessions found.</div>
        ) : (
          <div className="space-y-1">
            {sessions.map((session) => (
              <div 
                key={session.id}
                onClick={() => onSelectSession(session.id)}
                className={`p-3 rounded-lg cursor-pointer transition-all group ${
                  activeSessionId === session.id 
                    ? 'bg-[#313244]/40 border border-[#45475a]' 
                    : 'hover:bg-[#181825] border border-transparent hover:border-[#313244]'
                }`}
              >
                <div className="text-[10px] uppercase font-bold text-[#f38ba8] mb-1">LOCAL-HARNESS</div>
                <div className="text-[13px] font-medium text-[#cdd6f4] truncate mb-1">
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
