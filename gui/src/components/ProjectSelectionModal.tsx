import { useState, useEffect, useRef } from 'react';
import { Search, FolderOpen, Folder } from 'lucide-react';
import { invoke } from '@tauri-apps/api/core';
import { open } from '@tauri-apps/plugin-dialog';

export interface RecentProject {
  id: string;
  path: string;
  target_kind: string;
  target_host: string | null;
  target_user: string | null;
  target_port: number | null;
  target_key_path: string | null;
  last_opened_at: number;
}

interface ProjectSelectionModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSelectProject: (path: string, project?: RecentProject) => void;
}

export function ProjectSelectionModal({ isOpen, onClose, onSelectProject }: ProjectSelectionModalProps) {
  const [search, setSearch] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [projects, setProjects] = useState<RecentProject[]>([]);
  
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (isOpen) {
      setSearch('');
      setSelectedIndex(0);
      loadRecentProjects();
      // Small delay to allow CSS transition to start before focusing
      setTimeout(() => inputRef.current?.focus(), 50);
    }
  }, [isOpen]);

  const loadRecentProjects = async () => {
    try {
      const recent = await invoke<RecentProject[]>('get_recent_projects');
      setProjects(recent);
    } catch (e) {
      console.error("Failed to load recent projects:", e);
    }
  };

  const filteredProjects = projects.filter(p => 
    p.path.toLowerCase().includes(search.toLowerCase()) || 
    (p.target_host && p.target_host.toLowerCase().includes(search.toLowerCase()))
  );

  const handleBrowse = async () => {
    try {
      const selected = await open({
        directory: true,
        multiple: false,
      });
      if (selected && typeof selected === 'string') {
        onSelectProject(selected);
        onClose();
      }
    } catch (err) {
      console.error("Failed to open directory:", err);
    }
  };

  if (!isOpen) return null;

  return (
    <div 
      className="fixed inset-0 z-50 flex items-start justify-center pt-[15vh] bg-black/40 backdrop-blur-sm"
      onClick={onClose}
    >
      <div 
        className="w-[600px] bg-bg-primary rounded-xl shadow-2xl overflow-hidden border border-border-primary flex flex-col animate-in fade-in zoom-in-95 duration-200"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Search Header */}
        <div className="flex items-center px-4 py-3 border-b border-border-primary bg-bg-primary">
          <Search size={18} className="text-text-tertiary mr-3" />
          <input
            ref={inputRef}
            type="text"
            className="flex-1 bg-transparent text-text-primary text-[15px] outline-none placeholder:text-text-tertiary"
            placeholder="Select a directory or search recent projects..."
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
              setSelectedIndex(0);
            }}
            onKeyDown={(e) => {
              if (e.key === 'Escape') {
                e.preventDefault();
                onClose();
              } else if (e.key === 'ArrowDown') {
                e.preventDefault();
                setSelectedIndex(Math.min(selectedIndex + 1, filteredProjects.length));
              } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                setSelectedIndex(Math.max(selectedIndex - 1, 0));
              } else if (e.key === 'Enter') {
                e.preventDefault();
                if (selectedIndex === filteredProjects.length) {
                  handleBrowse();
                } else if (filteredProjects[selectedIndex]) {
                  onSelectProject(filteredProjects[selectedIndex].path, filteredProjects[selectedIndex]);
                  onClose();
                }
              }
            }}
          />
        </div>

        {/* List Content */}
        <div className="max-h-[400px] overflow-y-auto p-2 flex flex-col gap-1">
          {projects.length === 0 && search === '' ? (
            <div className="text-center py-8 text-sm text-text-tertiary">
              No recent projects found
            </div>
          ) : filteredProjects.length === 0 ? (
            <div className="text-center py-8 text-sm text-text-tertiary">
              No results match your search
            </div>
          ) : (
            filteredProjects.map((project, idx) => (
              <button
                key={project.id}
                onClick={() => {
                  onSelectProject(project.path, project);
                  onClose();
                }}
                onMouseEnter={() => setSelectedIndex(idx)}
                className={`w-full flex items-center justify-between px-3 py-2.5 rounded-md text-sm transition-colors ${
                  idx === selectedIndex
                    ? 'bg-blue-600 text-white'
                    : 'text-text-primary hover:bg-bg-tertiary'
                }`}
              >
                <div className="flex items-center gap-3 overflow-hidden">
                  <Folder size={16} className={idx === selectedIndex ? "opacity-100" : "opacity-70"} />
                  <div className="flex items-center gap-2 truncate">
                    <span className="font-medium truncate">{project.path.split(/[/\\]/).pop() || project.path}</span>
                    {project.target_kind === 'ssh' && (
                      <span className={`text-[10px] px-1.5 py-0.5 rounded ${idx === selectedIndex ? 'bg-blue-500 text-white border-none' : 'bg-bg-tertiary border border-border-primary'}`}>
                        SSH: {project.target_host}
                      </span>
                    )}
                    <span className={`text-xs truncate ${idx === selectedIndex ? 'text-blue-200' : 'text-text-tertiary'}`}>
                      {project.path}
                    </span>
                  </div>
                </div>
              </button>
            ))
          )}

          <button
            onClick={handleBrowse}
            onMouseEnter={() => setSelectedIndex(filteredProjects.length)}
            className={`w-full flex items-center justify-between px-3 py-2.5 rounded-md text-sm transition-colors ${
              selectedIndex === filteredProjects.length
                ? 'bg-blue-600 text-white'
                : 'text-text-primary hover:bg-bg-tertiary'
            }`}
          >
            <div className="flex items-center gap-3">
              <FolderOpen size={16} className={selectedIndex === filteredProjects.length ? "opacity-100" : "opacity-70"} />
              <span>Browse...</span>
            </div>
          </button>
        </div>

        {/* Footer */}
        <div className="bg-bg-tertiary px-4 py-2 border-t border-border-primary flex items-center justify-between text-[11px] text-text-tertiary">
          <div className="flex gap-4">
            <span>Use <b>↑</b> <b>↓</b> to navigate</span>
            <span><b>↵</b> to select</span>
            <span><b>Esc</b> to close</span>
          </div>
        </div>
      </div>
    </div>
  );
}
