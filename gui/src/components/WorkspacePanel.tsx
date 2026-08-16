import { useState, useMemo } from 'react';

import { EditorPanel } from './EditorPanel';

import { WorkspaceMenu } from './WorkspaceMenu';
import { FileExplorer } from './FileExplorer';

import { FileCode2, X } from 'lucide-react';
import { StepUpdate } from '../gen/localharness/v1/localharness_pb';
import { ConnectionTarget } from '../hooks/useHarness';

interface WorkspacePanelProps {
  steps: StepUpdate[];
  onNewSession: () => void;
  onOpenCustomizations?: () => void;
  connectionTarget?: ConnectionTarget | null;
}

interface OpenFile {
  path: string;
  content: string;
}

export function WorkspacePanel({ steps, onNewSession, onOpenCustomizations, connectionTarget }: WorkspacePanelProps) {
  const [openFiles, setOpenFiles] = useState<OpenFile[]>([]);
  const [activeFilePath, setActiveFilePath] = useState<string | null>(null);
  const [explorerOpen, setExplorerOpen] = useState(false);

  // Monitor steps to see if the agent modified a file. If so, open it!
  useMemo(() => {
    let latestModifiedFile: string | null = null;
    
    for (let i = steps.length - 1; i >= 0; i--) {
      const action = steps[i].action;
      if (!action?.case) continue;

      if (action.case === 'writeToFile' || action.case === 'replaceFileContent' || action.case === 'viewFile') {
        const val = action.value as any;
        latestModifiedFile = val.path || val.targetFile;
        break; // Only care about the absolute latest
      }
    }

    if (latestModifiedFile) {
      // Defer state update using a small timeout or wait for effect
      // Actually we should just update it in an effect to avoid render cycle warnings
    }
  }, [steps]);


  // Handle it safely via effect
  useState(() => {
    // We will let the user explicitly open files for now to keep things simple and stable,
    // or we can just pass the steps to EditorPanel like before.
    // Wait, EditorPanel already reads steps! 
    // We can just let EditorPanel manage the agent's files, and WorkspacePanel manage user's files.
    // But they share the same view. Let's pass user files down to EditorPanel!
  });

  const handleOpenFile = (path: string, content: string) => {
    setExplorerOpen(false);
    if (!openFiles.find(f => f.path === path)) {
      setOpenFiles([...openFiles, { path, content }]);
    }
    setActiveFilePath(path);
  };

  const closeFile = (path: string, e: React.MouseEvent) => {
    e.stopPropagation();
    const newFiles = openFiles.filter(f => f.path !== path);
    setOpenFiles(newFiles);
    if (activeFilePath === path) {
      setActiveFilePath(newFiles.length > 0 ? newFiles[newFiles.length - 1].path : null);
    }
  };

  const activeFileContent = openFiles.find(f => f.path === activeFilePath)?.content;

  // We consider it "empty" if there are no open files AND the agent hasn't done any steps yet?
  // Let's just use openFiles length for the empty state.
  // Wait! If the agent modifies a file, we want it to show up.
  // Let's just render EditorPanel if openFiles > 0 OR steps has actions.
  const hasAgentActions = steps.some(s => s.action?.case);
  const showEditor = openFiles.length > 0 || hasAgentActions;

  return (
    <div className="flex flex-col h-full bg-bg-primary border-l border-border-primary">
      <FileExplorer
        isOpen={explorerOpen}
        onClose={() => setExplorerOpen(false)}
        onFileSelect={handleOpenFile}
        connectionTarget={connectionTarget}
      />


      {/* Workspace Header & Tabs */}
      <div className="border-b border-border-primary flex flex-col shadow-sm z-10 bg-bg-primary">
        <div className="p-3 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <FileCode2 size={16} className="text-accent-primary" style={{ color: 'var(--accent-primary)' }} />
            <span className="font-semibold text-xs tracking-wide text-text-primary">Workspace</span>
          </div>
        </div>

        {/* File Tabs */}
        {openFiles.length > 0 && (
          <div className="flex items-center px-2 gap-1 overflow-x-auto scrollbar-none">
            {openFiles.map(file => {
              const name = file.path.split('/').pop() || file.path;
              const isActive = activeFilePath === file.path;
              return (
                <div
                  key={file.path}
                  onClick={() => setActiveFilePath(file.path)}
                  className={`group flex items-center gap-2 px-3 py-1.5 text-xs rounded-t-md cursor-pointer border-t border-l border-r border-transparent ${isActive ? 'bg-bg-tertiary text-accent-primary border-border-primary' : 'text-text-tertiary hover:bg-bg-secondary'}`}
                  style={isActive ? { color: 'var(--accent-primary)' } : {}}
                >
                  <span className="truncate max-w-[120px]">{name}</span>
                  <button onClick={(e) => closeFile(file.path, e)} className={`p-0.5 rounded-sm hover:bg-bg-tertiary ${isActive ? 'opacity-100' : 'opacity-0 group-hover:opacity-100'}`}>
                    <X size={12} />
                  </button>
                </div>
              );
            })}
          </div>
        )}
      </div>
      
      <div className="flex-1 overflow-hidden">
        {!showEditor ? (
          <WorkspaceMenu 
            onNewSession={onNewSession} 
            onOpenFile={() => setExplorerOpen(true)} 
            onOpenCustomizations={onOpenCustomizations || (() => {})}
            onViewDiffs={() => {}} 
          />
        ) : (
          <EditorPanel 
            steps={steps} 
            userActiveFile={activeFilePath} 
            userActiveContent={activeFileContent} 
          />
        )}
      </div>
    </div>
  );
}
