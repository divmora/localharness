import { Plus, FileText, SlidersHorizontal, Diff } from 'lucide-react';

interface WorkspaceMenuProps {
  onNewSession: () => void;
  onOpenFile: () => void;
  onOpenCustomizations: () => void;
  onViewDiffs: () => void;
}

export function WorkspaceMenu({ onNewSession, onOpenFile, onOpenCustomizations, onViewDiffs }: WorkspaceMenuProps) {
  return (
    <div className="flex flex-col h-full items-center justify-center bg-[#11111b] text-[#cdd6f4] p-8">
      <div className="w-full max-w-sm flex flex-col gap-6">
        
        <button onClick={onNewSession} className="flex gap-4 p-3 rounded-lg hover:bg-[#181825] transition-colors text-left group items-start">
          <Plus size={18} className="text-[#a6adc8] group-hover:text-[#cdd6f4] shrink-0 mt-0.5" />
          <div className="flex flex-col">
            <div className="flex items-center justify-between">
              <span className="text-sm font-semibold text-[#cdd6f4]">New session</span>
              <span className="text-xs font-mono text-[#6c7086]">⌘ T</span>
            </div>
            <span className="text-xs text-[#7f849c] mt-1">Start a new session in this space</span>
          </div>
        </button>

        <button onClick={onOpenFile} className="flex gap-4 p-3 rounded-lg hover:bg-[#181825] transition-colors text-left group items-start">
          <FileText size={18} className="text-[#a6adc8] group-hover:text-[#cdd6f4] shrink-0 mt-0.5" />
          <div className="flex flex-col">
            <div className="flex items-center justify-between">
              <span className="text-sm font-semibold text-[#cdd6f4]">Open file</span>
              <span className="text-xs font-mono text-[#6c7086]">⌘ P</span>
            </div>
            <span className="text-xs text-[#7f849c] mt-1">Open a file from the workspace</span>
          </div>
        </button>

        <button onClick={onOpenCustomizations} className="flex gap-4 p-3 rounded-lg hover:bg-[#181825] transition-colors text-left group items-start">
          <SlidersHorizontal size={18} className="text-[#a6adc8] group-hover:text-[#cdd6f4] shrink-0 mt-0.5" />
          <div className="flex flex-col">
            <span className="text-sm font-semibold text-[#cdd6f4]">Open customizations</span>
            <span className="text-xs text-[#7f849c] mt-1">Manage rules, skills, MCPs and plugins</span>
          </div>
        </button>

        <button onClick={onViewDiffs} className="flex gap-4 p-3 rounded-lg hover:bg-[#181825] transition-colors text-left group items-start opacity-70">
          <Diff size={18} className="text-[#a6adc8] group-hover:text-[#cdd6f4] shrink-0 mt-0.5" />
          <div className="flex flex-col">
            <span className="text-sm font-semibold text-[#cdd6f4]">View diffs</span>
            <span className="text-xs text-[#7f849c] mt-1">No changes available yet</span>
          </div>
        </button>

      </div>
    </div>
  );
}
