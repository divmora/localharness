import { Plus, MessageSquare, Terminal, FolderOpen, Cloud, TerminalSquare, ArrowUp, Mic } from 'lucide-react';
import { SessionInfo as ProtoSessionInfo } from '../gen/localharness/v1/localharness_pb';
import { useState } from 'react';
import { ProjectSelectionModal, RecentProject } from './ProjectSelectionModal';
import { invoke } from '@tauri-apps/api/core';
interface CenteredEmptyStateProps {
  onSelectSession: (id: string) => void;
  sessions: ProtoSessionInfo[];
  onSubmitPrompt: (prompt: string) => void;
  onOpenSessionsManager: () => void;
  workspace: string | null;
  onSelectWorkspace: (path: string) => void;
}

export function CenteredEmptyState({ onSelectSession, sessions, onSubmitPrompt, onOpenSessionsManager, workspace, onSelectWorkspace }: CenteredEmptyStateProps) {
  const [prompt, setPrompt] = useState("");
  const [isProjectModalOpen, setIsProjectModalOpen] = useState(false);

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

  const handleSelectProject = async (path: string, project?: RecentProject) => {
    onSelectWorkspace(path);
    
    // Save to recent projects
    try {
      const recent: RecentProject = {
        id: project?.id || `local:${path}`,
        path: path,
        target_kind: 'local',
        target_host: null,
        target_user: null,
        target_port: null,
        target_key_path: null,
        last_opened_at: Date.now()
      };
      await invoke('add_recent_project', { project: recent });
    } catch (e) {
      console.error("Failed to add to recent projects", e);
    }
  };


  return (
    <div className="flex-1 bg-bg-primary flex flex-col items-center overflow-y-auto">
      <div className="w-full max-w-3xl flex flex-col items-center mt-32 px-6 pb-20">

        {/* Logo / Hero */}
        <div className="mb-12 flex items-center justify-center">
          {/* A simple placeholder logo resembling three hexagons */}
          <div className="relative w-12 h-12 flex items-center justify-center opacity-40">
            <div className="absolute top-0 w-4 h-4 bg-text-tertiary rounded-sm transform rotate-45" style={{ backgroundColor: 'var(--text-tertiary)' }} />
            <div className="absolute bottom-2 left-1 w-4 h-4 bg-text-tertiary rounded-sm transform rotate-45" style={{ backgroundColor: 'var(--text-tertiary)' }} />
            <div className="absolute bottom-2 right-1 w-4 h-4 bg-text-tertiary rounded-sm transform rotate-45" style={{ backgroundColor: 'var(--text-tertiary)' }} />
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
              className="w-full bg-transparent text-text-primary placeholder:text-text-tertiary resize-none outline-none min-h-[60px]"
              autoFocus
            />

            <div className="flex items-center justify-between mt-2">
              <div className="flex items-center gap-2">
                <button className="p-1.5 rounded-md hover:bg-border-primary text-text-secondary transition-colors">
                  <Plus size={16} />
                </button>
                <div className="flex items-center gap-1.5 px-2 py-1 bg-border-primary rounded-md text-xs font-semibold text-success" style={{ color: 'var(--success)' }}>
                  <MessageSquare size={12} /> Ask
                </div>
                <span className="text-xs text-text-tertiary font-medium ml-2">SWE-1.6 Slow</span>
              </div>

              <div className="flex items-center gap-2">
                <div className="flex items-center gap-1.5 px-3 py-1.5 rounded-md hover:bg-border-primary text-xs font-semibold text-text-secondary cursor-pointer transition-colors">
                  <Terminal size={14} /> Local
                </div>
                <button className="p-1.5 rounded-md hover:bg-border-primary text-text-secondary transition-colors">
                  <Mic size={16} />
                </button>
                <button
                  onClick={handleSubmit}
                  disabled={!prompt.trim()}
                  className={`p-1.5 rounded-full flex items-center justify-center transition-colors ${prompt.trim()
                      ? 'bg-text-primary text-bg-primary hover:bg-text-secondary'
                      : 'bg-border-primary text-text-tertiary cursor-not-allowed'
                    }`}
                  style={prompt.trim() ? { backgroundColor: 'var(--text-primary)', color: 'var(--bg-primary)' } : {}}
                >
                  <ArrowUp size={16} strokeWidth={3} />
                </button>
              </div>
            </div>
          </div>

          <div className="bg-bg-secondary border-t border-border-primary px-4 py-3 flex items-center justify-between text-xs text-text-secondary">
            <div className="flex items-center gap-4">
              <div className="flex items-center gap-1.5">
                <TerminalSquare size={14} /> Local
              </div>
              <div className="flex items-center gap-1.5 relative">
                <button 
                  className="flex items-center gap-1.5 hover:text-text-primary transition-colors text-text-secondary" 
                  onClick={() => setIsProjectModalOpen(true)}
                >
                  <FolderOpen size={14} /> 
                  <span className="truncate max-w-[200px]">{workspace ? workspace.split(/[/\\]/).pop() || workspace : 'Select a directory...'}</span>
                </button>
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
        <div className="w-full max-w-2xl grid grid-cols-1 md:grid-cols-3 gap-3 mb-10">
          <div onClick={() => setIsProjectModalOpen(true)} className="bg-bg-secondary hover:bg-bg-tertiary border border-border-primary hover:border-border-highlight rounded-lg p-4 cursor-pointer transition-all flex flex-col gap-2">
            <FolderOpen size={16} className="text-text-secondary" />
            <span className="text-xs font-semibold text-text-primary">Open project</span>
          </div>
          <div className="bg-bg-secondary hover:bg-bg-tertiary border border-border-primary hover:border-border-highlight rounded-lg p-4 cursor-pointer transition-all flex flex-col gap-2">
            <Cloud size={16} className="text-text-secondary" />
            <span className="text-xs font-semibold text-text-primary">Clone repository</span>
          </div>
        </div>

        {/* Recent Sessions */}
        <div className="w-full max-w-2xl">
          <div className="flex items-center justify-between text-xs text-text-secondary mb-4">
            <span className="font-semibold">Recent sessions</span>
            <span className="text-accent-primary cursor-pointer hover:underline" style={{ color: 'var(--accent-primary)' }}>View all</span>
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
          Free • <span className="text-accent-primary cursor-pointer hover:underline" style={{ color: 'var(--accent-primary)' }}>Settings</span>
        </div>
      </div>
      
      <ProjectSelectionModal 
        isOpen={isProjectModalOpen} 
        onClose={() => setIsProjectModalOpen(false)} 
        onSelectProject={handleSelectProject} 
      />
    </div>
  );
}
