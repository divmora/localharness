import { useState, useMemo } from 'react';
import { TerminalSquare, User, Bot, Users, Globe, FileCode, ShieldAlert, Check, X, Send } from 'lucide-react';
import { motion } from 'framer-motion';
import { clone } from '@bufbuild/protobuf';
import { StepUpdate, StepUpdate_Source, StepUpdate_State, StepUpdateSchema } from '../gen/localharness/v1/localharness_pb';

interface ChatPanelProps {
  connected: boolean;
  connectionError?: string | null;
  steps: StepUpdate[];
  onSend: (text: string) => void;
  onSubmitQuestionResponse?: (requestId: string, answers: any[], skipped: boolean) => void;
  onSubmitPermissionResponse?: (requestId: string, approved: boolean, reason?: string) => void;
}

function QuestionForm({ action, state, onSubmit }: { action: any, state: StepUpdate_State, onSubmit: (answers: any[], skipped: boolean) => void }) {
  const [answers, setAnswers] = useState<any[]>(
    action.questions?.map(() => ({ selectedIndices: [], selectedOptions: [], text: '' })) || []
  );

  const isDone = state === StepUpdate_State.DONE;

  if (isDone) {
    if (action.skipped) return <div className="text-gray-400 italic text-xs mt-2">Skipped question.</div>;
    return (
      <div className="mt-2 flex flex-col gap-2">
        {action.questions?.map((q: any, i: number) => (
          <div key={i} className="bg-bg-tertiary p-2 rounded border border-border-primary">
            <div className="font-semibold text-xs mb-1">{q.question}</div>
            <div className="text-xs text-blue-400">
              {action.answers?.[i]?.selectedOptions?.length > 0 
                ? action.answers[i].selectedOptions.join(", ") 
                : action.answers?.[i]?.text || "No answer"}
            </div>
          </div>
        ))}
      </div>
    );
  }

  const handleToggle = (qIdx: number, optIdx: number, optText: string, isMulti: boolean) => {
    setAnswers(prev => {
      const next = [...prev];
      const cur = { ...next[qIdx] };
      if (isMulti) {
        if (cur.selectedIndices.includes(optIdx)) {
          cur.selectedIndices = cur.selectedIndices.filter((x: number) => x !== optIdx);
          cur.selectedOptions = cur.selectedOptions.filter((x: string) => x !== optText);
        } else {
          cur.selectedIndices = [...cur.selectedIndices, optIdx];
          cur.selectedOptions = [...cur.selectedOptions, optText];
        }
      } else {
        cur.selectedIndices = [optIdx];
        cur.selectedOptions = [optText];
      }
      next[qIdx] = cur;
      return next;
    });
  };

  return (
    <div className="mt-2 flex flex-col gap-3">
      {action.questions?.map((q: any, i: number) => (
        <div key={i} className="bg-bg-tertiary p-3 rounded border border-border-primary shadow-sm">
          <div className="font-semibold text-xs mb-2">{q.question}</div>
          {q.options && q.options.length > 0 ? (
            <div className="flex flex-col gap-1">
              {q.options.map((opt: string, optIdx: number) => (
                <label key={optIdx} className="flex items-center gap-2 text-xs cursor-pointer hover:text-text-primary">
                  <input 
                    type={q.isMultiSelect ? "checkbox" : "radio"} 
                    checked={answers[i].selectedIndices.includes(optIdx)}
                    onChange={() => handleToggle(i, optIdx, opt, q.isMultiSelect)}
                    className="accent-blue-500"
                  />
                  {opt}
                </label>
              ))}
            </div>
          ) : (
            <input 
              type="text" 
              className="w-full bg-bg-primary border border-border-primary rounded px-2 py-1 text-xs outline-none focus:border-blue-500" 
              placeholder="Type your answer..."
              value={answers[i].text}
              onChange={(e) => {
                const next = [...answers];
                next[i] = { ...next[i], text: e.target.value };
                setAnswers(next);
              }}
            />
          )}
        </div>
      ))}
      <div className="flex gap-2 justify-end mt-1">
        <button onClick={() => onSubmit([], true)} className="px-3 py-1 text-xs rounded bg-border-primary hover:bg-border-highlight transition-colors">Skip</button>
        <button onClick={() => onSubmit(answers, false)} className="px-3 py-1 text-xs rounded bg-blue-600 hover:bg-blue-500 transition-colors text-text-primary font-medium flex items-center gap-1"><Check size={12}/> Submit</button>
      </div>
    </div>
  );
}

export function ChatPanel({ 
  connected, 
  connectionError,
  steps, 
  onSend, 
  onSubmitQuestionResponse, 
  onSubmitPermissionResponse 
}: ChatPanelProps) {
  const [input, setInput] = useState('');

  // Group streaming steps by stepIndex so we have one cohesive message per step
  const messages = useMemo(() => {
    const grouped = new Map<number, StepUpdate>();
    
    for (const step of steps) {
      if (!grouped.has(step.stepIndex)) {
        grouped.set(step.stepIndex, clone(StepUpdateSchema, step));
      } else {
        const existing = grouped.get(step.stepIndex)!;
        // Merge text if it's a streaming update
        if (step.textDelta) {
          existing.text += step.textDelta;
        } else if (step.text) {
          existing.text = step.text;
        }
        
        // If the new step has an action (e.g. results), merge it
        if (step.action?.case) {
          existing.action = step.action;
        }
        // Always take the latest state
        existing.state = step.state;
        
        grouped.set(step.stepIndex, existing);
      }
    }
    
    return Array.from(grouped.values()).sort((a, b) => a.stepIndex - b.stepIndex);
  }, [steps]);

  const handleSend = () => {
    if (!input.trim() || !connected) return;
    onSend(input);
    setInput('');
  };

  const renderAction = (msg: StepUpdate) => {
    if (!msg.action?.case) return null;
    
    switch (msg.action.case) {
      case 'userQuestion':
        return (
          <QuestionForm 
            action={msg.action.value} 
            state={msg.state} 
            onSubmit={(answers, skipped) => {
              if (onSubmitQuestionResponse) {
                onSubmitQuestionResponse((msg.action!.value as any).requestId, answers, skipped);
              }
            }} 
          />
        );
      case 'invokeSubagent':
        const subagents = (msg.action.value as any).subagents || [];
        return (
          <div className="mt-2 flex flex-col gap-2">
            {subagents.map((sub: any, i: number) => (
              <div key={i} className="flex items-center gap-3 bg-bg-tertiary border border-blue-500/30 p-2 rounded shadow-sm">
                <div className="bg-blue-500/20 p-1.5 rounded text-blue-400">
                  <Users size={16} />
                </div>
                <div className="flex flex-col">
                  <span className="text-xs font-semibold text-blue-200">Spawned Subagent</span>
                  <span className="text-[10px] text-gray-400">{sub.role || sub.typeName}</span>
                </div>
                {msg.state !== StepUpdate_State.DONE && (
                  <div className="ml-auto flex items-center justify-center w-4 h-4">
                    <div className="animate-spin rounded-full h-3 w-3 border-t-2 border-b-2 border-blue-400"></div>
                  </div>
                )}
              </div>
            ))}
          </div>
        );
      case 'browserSubagent':
        const taskName = (msg.action.value as any).taskName || "Browser Task";
        return (
          <div className="mt-2 flex items-center gap-3 bg-bg-tertiary border border-purple-500/30 p-2 rounded shadow-sm">
            <div className="bg-purple-500/20 p-1.5 rounded text-purple-400">
              <Globe size={16} />
            </div>
            <div className="flex flex-col">
              <span className="text-xs font-semibold text-purple-200">Browser Agent</span>
              <span className="text-[10px] text-gray-400">{taskName}</span>
            </div>
            {msg.state !== StepUpdate_State.DONE && (
              <div className="ml-auto flex items-center justify-center w-4 h-4">
                <div className="animate-spin rounded-full h-3 w-3 border-t-2 border-b-2 border-purple-400"></div>
              </div>
            )}
          </div>
        );
      case 'permissionRequest':
        const pr = msg.action.value as any;
        return (
          <div className="mt-2 bg-[#EF4444]/10 border border-[#EF4444]/30 rounded p-3">
            <div className="flex items-center gap-2 text-[#EF4444] mb-2 font-semibold text-xs">
              <ShieldAlert size={16} />
              Permission Required: {pr.toolName}
            </div>
            <div className="text-[11px] text-text-primary bg-bg-primary p-2 rounded mb-3 font-mono">
              {pr.argsSummary || pr.argsJson}
            </div>
            {msg.state !== StepUpdate_State.DONE ? (
              <div className="flex items-center gap-2">
                <button 
                  onClick={() => onSubmitPermissionResponse && onSubmitPermissionResponse(pr.requestId, true)}
                  className="flex items-center gap-1 bg-green-600/20 hover:bg-green-600/40 text-green-400 px-3 py-1 rounded text-xs transition-colors"
                >
                  <Check size={12} /> Approve
                </button>
                <button 
                  onClick={() => onSubmitPermissionResponse && onSubmitPermissionResponse(pr.requestId, false, "Denied by user")}
                  className="flex items-center gap-1 bg-red-600/20 hover:bg-red-600/40 text-red-400 px-3 py-1 rounded text-xs transition-colors"
                >
                  <X size={12} /> Deny
                </button>
              </div>
            ) : (
              <div className="text-[11px] text-text-secondary italic">Response submitted.</div>
            )}
          </div>
        );
      case 'writeToFile':
      case 'replaceFileContent':
        const fileAction = msg.action.value as any;
        const isArtifact = fileAction.isArtifact || false;
        
        if (isArtifact) {
          const summary = fileAction.artifactMetadata?.summary || fileAction.description || "Updated artifact";
          const fileName = fileAction.targetFile?.split('/').pop() || fileAction.path?.split('/').pop() || "file";
          
          return (
            <div className="mt-2 flex items-center gap-3 bg-bg-tertiary border border-blue-400/30 p-2.5 rounded shadow-sm">
              <div className="bg-blue-400/20 p-2 rounded text-blue-400">
                <FileCode size={18} />
              </div>
              <div className="flex flex-col flex-1 overflow-hidden">
                <span className="text-xs font-semibold text-blue-300 truncate">Artifact: {fileName}</span>
                <span className="text-[11px] text-gray-400 truncate">{summary}</span>
              </div>
            </div>
          );
        }
        return <div className="text-[10px] text-text-tertiary italic mt-1 font-mono">Edited {fileAction.targetFile || fileAction.path}</div>;
      default:
        return <div className="text-xs text-text-tertiary italic mt-1">[Tool: {msg.action.case}]</div>;
    }
  };

  return (
    <div className="flex flex-col h-full bg-bg-secondary text-text-primary">
      <div className="p-4 border-b border-border-primary font-semibold flex items-center justify-between shadow-sm z-10">
        <div className="flex items-center gap-2">
          <TerminalSquare size={18} className="text-blue-400" />
          LocalHarness
        </div>
        <div className="flex items-center gap-2 text-xs">
          <div className={`w-2 h-2 rounded-full ${connected ? 'bg-green-500' : 'bg-red-500 shadow-[0_0_8px_rgba(239,68,68,0.8)]'}`} />
          {connected ? 'Connected' : 'Disconnected'}
        </div>
      </div>
      
      <div className="flex-1 overflow-y-auto p-4 space-y-4 flex flex-col scroll-smooth">
        {messages.length === 0 && (
          <div className="text-center text-[#585b70] mt-10 text-sm flex flex-col items-center gap-2 opacity-50">
            <Bot size={32} />
            Ready to chat. Ask me anything!
          </div>
        )}
        
        {messages.map((msg) => {
          const isUser = msg.source === StepUpdate_Source.USER;
          const hasText = !!msg.text;
          
          return (
            <motion.div 
              key={msg.stepIndex}
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              className={`p-3 rounded-lg max-w-[95%] text-sm shadow-md flex flex-col gap-1 ${
                isUser 
                  ? 'bg-blue-600/20 text-blue-100 self-end border border-blue-500/30' 
                  : 'bg-border-primary self-start border border-border-highlight/50'
              }`}
            >
              <div className="flex items-center gap-2 mb-1 text-[11px] font-semibold opacity-70 uppercase tracking-wider">
                {isUser ? <User size={12} /> : <Bot size={12} />}
                {isUser ? 'You' : 'Agent'}
              </div>
              
              {msg.thinking && (
                <details className="mt-1 mb-2 group">
                  <summary className="text-[11px] font-semibold text-text-secondary cursor-pointer hover:text-text-primary transition-colors select-none flex items-center gap-1">
                    <span className="w-1 h-1 rounded-full bg-blue-400 opacity-50 group-hover:opacity-100 transition-opacity"></span>
                    Thought Process
                  </summary>
                  <div className="mt-2 text-[11px] text-[#bac2de] bg-bg-primary/50 p-3 rounded border border-border-primary whitespace-pre-wrap font-mono leading-relaxed max-h-[300px] overflow-y-auto scrollbar-thin">
                    {msg.thinking}
                  </div>
                </details>
              )}
              
              {hasText && <div className="whitespace-pre-wrap leading-relaxed">{msg.text}</div>}
              
              {renderAction(msg)}
              
            </motion.div>
          );
        })}
        {connectionError && (
          <div className="bg-[#7f1d1d]/20 border border-[#991b1b] rounded p-4 flex flex-col gap-2 my-2">
            <div className="flex items-center gap-2 text-red-400 font-semibold text-sm">
              <ShieldAlert size={16} /> Connection Error
            </div>
            <div className="text-xs text-red-300 font-mono whitespace-pre-wrap break-all">
              {connectionError}
            </div>
            {connectionError.includes("No valid LLM configuration found") && (
              <div className="text-xs text-[#D1D5DB] mt-1 border-t border-[#991b1b]/50 pt-2">
                Tip: Use the Customizations panel (gear icon) to configure your LLM endpoint.
              </div>
            )}
          </div>
        )}
      </div>
      
      <div className="p-4 border-t border-border-primary">
        <div className="flex items-center bg-bg-tertiary rounded-md border border-border-primary focus-within:border-blue-500 transition-colors px-3 py-2 shadow-inner">
          <input 
            type="text"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                handleSend();
              }
            }}
            disabled={!connected || !!connectionError}
            placeholder={connected ? "Ask anything..." : connectionError ? "Cannot send messages due to error" : "Connecting..."}
            className={`flex-1 bg-transparent border-none outline-none text-sm text-text-primary placeholder:text-text-tertiary ${connected ? '' : 'opacity-50'}`}
          />
          <button 
            onClick={handleSend}
            disabled={!connected || !input.trim() || !!connectionError}
            className="text-blue-400 hover:text-blue-300 ml-2 p-1 rounded hover:bg-border-primary transition-colors disabled:opacity-50 disabled:hover:bg-transparent"
          >
            <Send size={16} />
          </button>
        </div>
      </div>
    </div>
  );
}
