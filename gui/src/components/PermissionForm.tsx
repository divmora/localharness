import { useState } from 'react';
import { Square, Copy, X, Wand2, Check, ChevronDown } from 'lucide-react';
import { StepUpdate_State } from '../gen/localharness/v1/localharness_pb';

interface PermissionFormProps {
  pr: any;
  state: StepUpdate_State;
  onSubmitPermissionResponse?: (requestId: string, approved: boolean, reason?: string) => void;
}

export function PermissionForm({ pr, state, onSubmitPermissionResponse }: PermissionFormProps) {
  const [submitted, setSubmitted] = useState(false);

  const handleAllow = () => {
    if (onSubmitPermissionResponse) onSubmitPermissionResponse(pr.requestId, true);
    setSubmitted(true);
  };

  const handleReject = () => {
    if (onSubmitPermissionResponse) onSubmitPermissionResponse(pr.requestId, false, "Denied by user");
    setSubmitted(true);
  };

  return (
    <div className="mt-3 border border-blue-400 rounded-xl overflow-hidden shadow-sm max-w-full bg-blue-500/5">
      <div className="flex items-center justify-between px-3 py-2 text-xs text-text-primary bg-blue-500/10 border-b border-blue-400/30 font-medium">
        <div className="flex items-center gap-2">
          <Square size={12} className="text-blue-500" />
          <span className="truncate">Command {pr.toolName}</span>
        </div>
        <div className="flex items-center gap-2 text-text-tertiary">
          <Copy size={12} className="cursor-pointer hover:text-text-primary transition-colors" />
          <X size={12} className="cursor-pointer hover:text-text-primary transition-colors" />
        </div>
      </div>
      <div className="p-4 text-[11px] font-mono overflow-x-auto">
        <div className="flex items-center gap-2 mb-2 text-blue-400 font-semibold">
          <div className="w-2 h-2 rounded-full border border-blue-400 bg-transparent"></div>
          {pr.argsSummary || pr.argsJson}
        </div>
        {(state !== StepUpdate_State.DONE && !submitted) ? (
          <div className="flex justify-end items-center gap-2 mt-4 text-xs font-sans">
            <button className="p-1.5 rounded-md bg-gray-200 dark:bg-gray-700/50 text-text-secondary hover:text-text-primary hover:bg-gray-300 dark:hover:bg-gray-700 transition-colors">
              <Wand2 size={14} />
            </button>
            <div className="flex items-center rounded-md overflow-hidden bg-blue-500 hover:bg-blue-600 text-white font-medium shadow-sm cursor-pointer transition-colors">
              <button 
                onClick={handleAllow}
                className="px-3 py-1.5 flex items-center gap-1 flex-1"
              >
                Allow <Check size={12} className="ml-1 opacity-70" />
              </button>
              <div className="w-px h-full min-h-[28px] bg-blue-400/50"></div>
              <button className="px-1.5 py-1.5 hover:bg-blue-700 transition-colors flex items-center justify-center">
                <ChevronDown size={14} />
              </button>
            </div>
            <button 
              onClick={handleReject}
              className="flex items-center gap-1 bg-gray-200 dark:bg-gray-700/80 text-gray-800 dark:text-gray-200 px-3 py-1.5 rounded-md hover:bg-gray-300 dark:hover:bg-gray-600 transition-colors font-medium shadow-sm"
            >
              Reject <X size={12} className="opacity-70" />
            </button>
          </div>
        ) : (
          <div className="text-[11px] text-text-secondary italic mt-2 text-right opacity-70">Response submitted</div>
        )}
      </div>
    </div>
  );
}
