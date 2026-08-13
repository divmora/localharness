import { useState, useMemo } from 'react';
import { Panel, Group as PanelGroup, Separator as PanelResizeHandle } from 'react-resizable-panels';
import { EditorPanel } from './EditorPanel';
import { TerminalPanel } from './TerminalPanel';
import { WorkspaceMenu } from './WorkspaceMenu';
import { FileExplorer } from './FileExplorer';
import { FileCode2, Terminal as TerminalIcon, X } from 'lucide-react';
import { StepUpdate } from '../gen/localharness/v1/localharness_pb';

interface WorkspacePanelProps {
  steps: StepUpdate[];
}

interface OpenFile {
  path: string;
  content: string;
}

export function WorkspacePanel({ steps }: WorkspacePanelProps) {
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
    <div className="flex flex-col h-full bg-[#11111b] border-l border-[#181825]">
      <FileExplorer 
        isOpen={explorerOpen} 
        onClose={() => setExplorerOpen(false)} 
        onFileSelect={handleOpenFile} 
      />

      {/* Workspace Header & Tabs */}
      <div className="border-b border-[#181825] flex flex-col shadow-sm z-10 bg-[#11111b]">
        <div className="p-3 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <FileCode2 size={16} className="text-[#89b4fa]" />
            <span className="font-semibold text-xs tracking-wide text-[#cdd6f4]">Workspace</span>
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
                  className={`group flex items-center gap-2 px-3 py-1.5 text-xs rounded-t-md cursor-pointer border-t border-l border-r border-transparent ${isActive ? 'bg-[#1e1e2e] text-[#89b4fa] border-[#313244]' : 'text-[#a6adc8] hover:bg-[#181825]'}`}
                >
                  <span className="truncate max-w-[120px]">{name}</span>
                  <button onClick={(e) => closeFile(file.path, e)} className={`p-0.5 rounded-sm hover:bg-[#313244] ${isActive ? 'opacity-100' : 'opacity-0 group-hover:opacity-100'}`}>
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
            onNewSession={() => {}} 
            onOpenFile={() => setExplorerOpen(true)} 
            onOpenCustomizations={() => {}} 
            onViewDiffs={() => {}} 
          />
        ) : (
          <PanelGroup orientation="vertical">
            <Panel defaultSize={70} className="relative bg-[#1e1e2e]">
              <EditorPanel 
                steps={steps} 
                userActiveFile={activeFilePath} 
                userActiveContent={activeFileContent} 
              />
            </Panel>
            
            <PanelResizeHandle className="h-1 bg-[#181825] hover:bg-[#89b4fa]/50 transition-colors z-10" />
            
            <Panel defaultSize={30} minSize={10} className="relative bg-[#181825]">
              <div className="absolute top-0 left-0 right-0 bg-[#11111b] px-3 py-1.5 border-b border-[#313244] z-10 flex items-center gap-2">
                 <TerminalIcon size={12} className="text-[#a6adc8]" />
                 <span className="text-[10px] font-semibold text-[#a6adc8] uppercase tracking-wider">Terminal Output</span>
              </div>
              <div className="pt-8 h-full">
                <TerminalPanel steps={steps} />
              </div>
            </Panel>
          </PanelGroup>
        )}
      </div>
    </div>
  );
}
