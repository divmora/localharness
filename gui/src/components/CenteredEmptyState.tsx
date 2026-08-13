import { Plus, MessageSquare, Terminal, FolderOpen, Cloud, TerminalSquare, ArrowUp, Mic, ChevronDown } from 'lucide-react';
import { SessionInfo as ProtoSessionInfo } from '../gen/localharness/v1/localharness_pb';
import { useState, useRef, useEffect, useMemo } from 'react';
import { ConnectionTarget } from '../hooks/useHarness';
import { open } from '@tauri-apps/plugin-dialog';

interface CenteredEmptyStateProps {
  onSelectSession: (id: string) => void;
  sessions: ProtoSessionInfo[];
  onSubmitPrompt: (prompt: string) => void;
  onOpenSSHModal: () => void;
  onOpenSessionsManager: () => void;
  connectionTarget?: ConnectionTarget | null;
  workspace: string | null;
  onSelectWorkspace: (path: string) => void;
}

export function CenteredEmptyState({ onSelectSession, sessions, onSubmitPrompt, onOpenSSHModal, onOpenSessionsManager, connectionTarget, workspace, onSelectWorkspace }: CenteredEmptyStateProps) {
  const [prompt, setPrompt] = useState("");
  const [isWorkspaceDropdownOpen, setIsWorkspaceDropdownOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsWorkspaceDropdownOpen(false);
      }
    };
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  const recentWorkspaces = useMemo(() => {
    const wsList = sessions.map(s => s.workspace).filter(Boolean);
    return Array.from(new Set(wsList)).slice(0, 5); // top 5 unique workspaces
  }, [sessions]);

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
  const handleSelectDirectory = async () => {
    try {
      const selected = await open({
        directory: true,
        multiple: false,
      });
      if (selected && typeof selected === 'string') {
        onSelectWorkspace(selected);
        setIsWorkspaceDropdownOpen(false);
      }
    } catch (err) {
      console.error("Failed to open dialog:", err);
    }
  };

  return (
    <div className="flex-1 bg-bg-primary flex flex-col items-center overflow-y-auto">
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
        <div className="w-full bg-bg-tertiary border border-border-primary rounded-xl overflow-hidden shadow-2xl flex flex-col mb-4">
          <div className="px-4 py-3 border-b border-border-primary flex items-center text-xs text-text-secondary font-medium">
            Tip: Try typing "megaplan" to plan deeply before building
          </div>

          <div className="p-4 flex flex-col">
            <textarea
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="What would you like to build?"
              className="w-full bg-transparent text-text-primary placeholder-[#6B7280] resize-none outline-none min-h-[60px]"
              autoFocus
            />

            <div className="flex items-center justify-between mt-2">
              <div className="flex items-center gap-2">
                <button className="p-1.5 rounded-md hover:bg-border-primary text-text-secondary transition-colors">
                  <Plus size={16} />
                </button>
                <div className="flex items-center gap-1.5 px-2 py-1 bg-border-primary rounded-md text-xs font-semibold text-[#10B981]">
                  <MessageSquare size={12} /> Ask
                </div>
                <span className="text-xs text-text-tertiary font-medium ml-2">SWE-1.6 Slow</span>
              </div>

              <div className="flex items-center gap-2">
                <div className="flex items-center gap-1.5 px-3 py-1.5 rounded-md hover:bg-border-primary text-xs font-semibold text-text-secondary cursor-pointer transition-colors">
                  <Terminal size={14} /> {connectionTarget?.kind === 'ssh' ? `SSH: ${connectionTarget.host}` : 'Local'}
                </div>
                <button className="p-1.5 rounded-md hover:bg-border-primary text-text-secondary transition-colors">
                  <Mic size={16} />
                </button>
                <button
                  onClick={handleSubmit}
                  disabled={!prompt.trim()}
                  className={`p-1.5 rounded-full flex items-center justify-center transition-colors ${prompt.trim()
                      ? 'bg-[#F9FAFB] text-[#000000] hover:bg-[#E5E7EB]'
                      : 'bg-border-primary text-text-tertiary cursor-not-allowed'
                    }`}
                >
                  <ArrowUp size={16} strokeWidth={3} />
                </button>
              </div>
            </div>
          </div>

          <div className="bg-bg-secondary border-t border-border-primary px-4 py-3 flex items-center justify-between text-xs text-text-secondary">
            <div className="flex items-center gap-4">
              <div className="flex items-center gap-1.5">
                <TerminalSquare size={14} /> {connectionTarget?.kind === 'ssh' ? `SSH: ${connectionTarget.host}` : 'Local'}
              </div>
              <div className="flex items-center gap-1.5 relative" ref={dropdownRef}>
                <div 
                  className="flex items-center gap-1.5 cursor-pointer hover:text-text-primary transition-colors text-text-secondary" 
                  onClick={() => setIsWorkspaceDropdownOpen(!isWorkspaceDropdownOpen)}
                >
                  <FolderOpen size={14} /> 
                  <span className="truncate max-w-[200px]">{workspace ? workspace.split('/').pop() || workspace : 'Select a directory...'}</span>
                  <ChevronDown size={14} className="ml-0.5 opacity-70" />
                </div>
                
                {isWorkspaceDropdownOpen && (
                  <div className="absolute top-full left-0 mt-2 w-72 bg-bg-primary border border-border-primary rounded-lg shadow-xl z-50 overflow-hidden flex flex-col">
                    <div className="px-3 py-2 text-xs font-semibold text-text-secondary border-b border-border-primary bg-bg-secondary">
                      Select a directory
                    </div>
                    
                    <div className="max-h-48 overflow-y-auto">
                      {recentWorkspaces.map((path, idx) => (
                        <div 
                          key={idx}
                          className="px-3 py-2 text-xs text-text-primary hover:bg-bg-tertiary cursor-pointer flex items-center justify-between group"
                          onClick={() => {
                            onSelectWorkspace(path);
                            setIsWorkspaceDropdownOpen(false);
                          }}
                        >
                          <span className="font-medium truncate max-w-[120px]">{path.split('/').pop()}</span>
                          <span className="text-[10px] text-text-tertiary truncate max-w-[120px] group-hover:text-text-secondary">{path}</span>
                        </div>
                      ))}
                    </div>
                    
                    <div 
                      className="px-3 py-2.5 text-xs text-text-primary hover:bg-bg-tertiary cursor-pointer border-t border-border-primary flex items-center gap-2"
                      onClick={handleSelectDirectory}
                    >
                      <FolderOpen size={14} className="text-text-secondary" />
                      Browse...
                    </div>
                  </div>
                )}
              </div>
            </div>
            <div 
              className="cursor-pointer hover:text-text-primary transition-colors flex items-center gap-1"
              onClick={onOpenSessionsManager}
            >
              Go to agent manager <ArrowUp size={12} className="transform rotate-45" />
            </div>
          </div>
        </div>

        {/* Action Cards */}
        <div className="w-full grid grid-cols-3 gap-4 mb-10">
          <div className="bg-bg-secondary hover:bg-bg-tertiary border border-border-primary hover:border-border-highlight rounded-lg p-4 cursor-pointer transition-all flex flex-col gap-2">
            <FolderOpen size={16} className="text-text-secondary" />
            <span className="text-xs font-semibold text-text-primary">Open project</span>
          </div>
          <div className="bg-bg-secondary hover:bg-bg-tertiary border border-border-primary hover:border-border-highlight rounded-lg p-4 cursor-pointer transition-all flex flex-col gap-2">
            <Cloud size={16} className="text-text-secondary" />
            <span className="text-xs font-semibold text-text-primary">Clone repository</span>
          </div>
          {(!connectionTarget || connectionTarget.kind !== 'ssh') && (
            <div onClick={onOpenSSHModal} className="bg-bg-secondary hover:bg-bg-tertiary border border-border-primary hover:border-border-highlight rounded-lg p-4 cursor-pointer transition-all flex flex-col gap-2">
              <TerminalSquare size={16} className="text-text-secondary" />
              <span className="text-xs font-semibold text-text-primary">Connect via SSH</span>
            </div>
          )}
        </div>

        {/* Recent Sessions */}
        <div className="w-full max-w-2xl">
          <div className="flex items-center justify-between text-xs text-text-secondary mb-4">
            <span className="font-semibold">Recent sessions</span>
            <span className="text-[#3B82F6] cursor-pointer hover:underline">View all</span>
          </div>

          <div className="flex flex-col border border-border-primary rounded-lg bg-bg-secondary overflow-hidden">
            {sessions.slice(0, 5).map((session, i) => (
              <div
                key={session.id}
                onClick={() => onSelectSession(session.id)}
                className={`flex items-center justify-between p-3 cursor-pointer hover:bg-bg-tertiary transition-colors ${i < sessions.length - 1 ? 'border-b border-border-primary' : ''
                  }`}
              >
                <div className="flex items-center gap-3">
                  <span className="text-sm font-semibold text-text-primary">{session.name || "Untitled session"}</span>
                  <span className="text-[10px] text-text-tertiary">
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
              <div className="p-6 text-center text-xs text-text-tertiary">
                No recent sessions found.
              </div>
            )}
          </div>
        </div>

        {/* Footer Link */}
        <div className="mt-auto pt-8 flex items-center text-xs text-text-tertiary font-medium gap-1">
          Free • <span className="text-[#3B82F6] cursor-pointer hover:underline">Settings</span>
        </div>
      </div>
    </div>
  );
}
