import { Panel, Group as PanelGroup, Separator as PanelResizeHandle } from 'react-resizable-panels';
import { EditorPanel } from './EditorPanel';
import { TerminalPanel } from './TerminalPanel';
import { FileCode2, Terminal as TerminalIcon } from 'lucide-react';

interface WorkspacePanelProps {
  steps: any[];
}

export function WorkspacePanel({ steps }: WorkspacePanelProps) {
  return (
    <div className="flex flex-col h-full bg-[#11111b] border-l border-[#181825]">
      {/* Workspace Header */}
      <div className="p-3 border-b border-[#181825] flex items-center justify-between shadow-sm z-10 bg-[#11111b]">
        <div className="flex items-center gap-2">
          <FileCode2 size={16} className="text-[#89b4fa]" />
          <span className="font-semibold text-xs tracking-wide text-[#cdd6f4]">Workspace</span>
        </div>
      </div>
      
      <div className="flex-1 overflow-hidden">
        <PanelGroup orientation="vertical">
          <Panel defaultSize={70} className="relative bg-[#1e1e2e]">
            <EditorPanel steps={steps} />
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
      </div>
    </div>
  );
}
