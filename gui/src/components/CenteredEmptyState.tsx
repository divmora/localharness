import { Plus, MessageSquare, Terminal, FolderOpen, Cloud, TerminalSquare, ArrowUp, Mic } from 'lucide-react';
import { SessionInfo as ProtoSessionInfo } from '../gen/localharness/v1/localharness_pb';
import { useState } from 'react';
import { ConnectionTarget } from '../hooks/useHarness';

interface CenteredEmptyStateProps {
  onSelectSession: (id: string) => void;
  sessions: ProtoSessionInfo[];
  onSubmitPrompt: (prompt: string) => void;
  onOpenSSHModal: () => void;
  connectionTarget?: ConnectionTarget | null;
}

export function CenteredEmptyState({ onSelectSession, sessions, onSubmitPrompt, onOpenSSHModal, connectionTarget }: CenteredEmptyStateProps) {
  const [prompt, setPrompt] = useState("");

  const handleSubmit = () => {
    if (prompt.trim()) {
      onSubmitPrompt(prompt);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSubmit();
    }
  };

  return (
    <div className="flex-1 bg-[#000000] flex flex-col items-center overflow-y-auto">
      <div className="w-full max-w-3xl flex flex-col items-center mt-32 px-6 pb-20">

        {/* Logo / Hero */}
        <div className="mb-12 flex items-center justify-center">
          {/* A simple placeholder logo resembling three hexagons */}
          <div className="relative w-12 h-12 flex items-center justify-center opacity-40">
            <div className="absolute top-0 w-4 h-4 bg-[#6B7280] rounded-sm transform rotate-45" />
            <div className="absolute bottom-2 left-1 w-4 h-4 bg-[#6B7280] rounded-sm transform rotate-45" />
            <div className="absolute bottom-2 right-1 w-4 h-4 bg-[#6B7280] rounded-sm transform rotate-45" />
          </div>
        </div>

        {/* Input Box */}
        <div className="w-full bg-[#121212] border border-[#262626] rounded-xl overflow-hidden shadow-2xl flex flex-col mb-4">
          <div className="px-4 py-3 border-b border-[#262626] flex items-center text-xs text-[#9CA3AF] font-medium">
            Tip: Try typing "megaplan" to plan deeply before building
          </div>

          <div className="p-4 flex flex-col">
            <textarea
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="What would you like to build?"
              className="w-full bg-transparent text-[#F9FAFB] placeholder-[#6B7280] resize-none outline-none min-h-[60px]"
              autoFocus
            />

            <div className="flex items-center justify-between mt-2">
              <div className="flex items-center gap-2">
                <button className="p-1.5 rounded-md hover:bg-[#262626] text-[#9CA3AF] transition-colors">
                  <Plus size={16} />
                </button>
                <div className="flex items-center gap-1.5 px-2 py-1 bg-[#262626] rounded-md text-xs font-semibold text-[#10B981]">
                  <MessageSquare size={12} /> Ask
                </div>
                <span className="text-xs text-[#6B7280] font-medium ml-2">SWE-1.6 Slow</span>
              </div>

              <div className="flex items-center gap-2">
                <div className="flex items-center gap-1.5 px-3 py-1.5 rounded-md hover:bg-[#262626] text-xs font-semibold text-[#9CA3AF] cursor-pointer transition-colors">
                  <Terminal size={14} /> {connectionTarget?.kind === 'ssh' ? `SSH: ${connectionTarget.host}` : 'Local'}
                </div>
                <button className="p-1.5 rounded-md hover:bg-[#262626] text-[#9CA3AF] transition-colors">
                  <Mic size={16} />
                </button>
                <button
                  onClick={handleSubmit}
                  disabled={!prompt.trim()}
                  className={`p-1.5 rounded-full flex items-center justify-center transition-colors ${prompt.trim()
                      ? 'bg-[#F9FAFB] text-[#000000] hover:bg-[#E5E7EB]'
                      : 'bg-[#262626] text-[#6B7280] cursor-not-allowed'
                    }`}
                >
                  <ArrowUp size={16} strokeWidth={3} />
                </button>
              </div>
            </div>
          </div>

          <div className="bg-[#0A0A0A] border-t border-[#262626] px-4 py-3 flex items-center justify-between text-xs text-[#9CA3AF]">
            <div className="flex items-center gap-4">
              <div className="flex items-center gap-1.5">
                <TerminalSquare size={14} /> {connectionTarget?.kind === 'ssh' ? `SSH: ${connectionTarget.host}` : 'Local'}
              </div>
              <div className="flex items-center gap-1.5 cursor-pointer hover:text-[#F9FAFB] transition-colors">
                <FolderOpen size={14} /> Select a directory...
              </div>
            </div>
            <div className="cursor-pointer hover:text-[#F9FAFB] transition-colors flex items-center gap-1">
              Go to agent manager <ArrowUp size={12} className="transform rotate-45" />
            </div>
          </div>
        </div>

        {/* Action Cards */}
        <div className="w-full grid grid-cols-3 gap-4 mb-10">
          <div className="bg-[#0A0A0A] hover:bg-[#121212] border border-[#262626] hover:border-[#333333] rounded-lg p-4 cursor-pointer transition-all flex flex-col gap-2">
            <FolderOpen size={16} className="text-[#9CA3AF]" />
            <span className="text-xs font-semibold text-[#F9FAFB]">Open project</span>
          </div>
          <div className="bg-[#0A0A0A] hover:bg-[#121212] border border-[#262626] hover:border-[#333333] rounded-lg p-4 cursor-pointer transition-all flex flex-col gap-2">
            <Cloud size={16} className="text-[#9CA3AF]" />
            <span className="text-xs font-semibold text-[#F9FAFB]">Clone repository</span>
          </div>
          {(!connectionTarget || connectionTarget.kind !== 'ssh') && (
            <div onClick={onOpenSSHModal} className="bg-[#0A0A0A] hover:bg-[#121212] border border-[#262626] hover:border-[#333333] rounded-lg p-4 cursor-pointer transition-all flex flex-col gap-2">
              <TerminalSquare size={16} className="text-[#9CA3AF]" />
              <span className="text-xs font-semibold text-[#F9FAFB]">Connect via SSH</span>
            </div>
          )}
        </div>

        {/* Recent Sessions */}
        <div className="w-full max-w-2xl">
          <div className="flex items-center justify-between text-xs text-[#9CA3AF] mb-4">
            <span className="font-semibold">Recent sessions</span>
            <span className="text-[#3B82F6] cursor-pointer hover:underline">View all</span>
          </div>

          <div className="flex flex-col border border-[#262626] rounded-lg bg-[#0A0A0A] overflow-hidden">
            {sessions.slice(0, 5).map((session, i) => (
              <div
                key={session.id}
                onClick={() => onSelectSession(session.id)}
                className={`flex items-center justify-between p-3 cursor-pointer hover:bg-[#121212] transition-colors ${i < sessions.length - 1 ? 'border-b border-[#262626]' : ''
                  }`}
              >
                <div className="flex items-center gap-3">
                  <span className="text-sm font-semibold text-[#F9FAFB]">{session.name || "Untitled session"}</span>
                  <span className="text-[10px] text-[#6B7280]">
                    • {(() => {
                      const diff = Date.now() - (Number(session.updatedAt) * 1000);
                      const minutes = Math.floor(diff / 60000);
                      if (minutes < 60) return `${minutes}m ago`;
                      const hours = Math.floor(minutes / 60);
                      if (hours < 24) return `${hours}h ago`;
                      return `${Math.floor(hours / 24)}d ago`;
                    })()}
                  </span>
                </div>
              </div>
            ))}
            {sessions.length === 0 && (
              <div className="p-6 text-center text-xs text-[#6B7280]">
                No recent sessions found.
              </div>
            )}
          </div>
        </div>

        {/* Footer Link */}
        <div className="mt-auto pt-8 flex items-center text-xs text-[#6B7280] font-medium gap-1">
          Free • <span className="text-[#3B82F6] cursor-pointer hover:underline">Settings</span>
        </div>
      </div>
    </div>
  );
}
