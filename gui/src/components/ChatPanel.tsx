import { useState, useMemo } from 'react';
import { TerminalSquare, Bot, Users, Globe, FileCode, ShieldAlert, Check, X, Brain, ThumbsUp, ThumbsDown, Copy, GitFork, BarChart2, MoreHorizontal, Square, ChevronDown, Plus, MessageCircle, Mic, ArrowUp, Monitor, Folder, Server, Wand2 } from 'lucide-react';
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
    if (action.skipped) return <div className="text-text-tertiary italic text-xs mt-2">Skipped question.</div>;
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
                  <span className="text-[10px] text-text-tertiary">{sub.role || sub.typeName}</span>
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
              <span className="text-[10px] text-text-tertiary">{taskName}</span>
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
              {msg.state !== StepUpdate_State.DONE ? (
                <div className="flex justify-end items-center gap-2 mt-4 text-xs font-sans">
                  <button className="p-1.5 rounded-md bg-gray-200 dark:bg-gray-700/50 text-text-secondary hover:text-text-primary hover:bg-gray-300 dark:hover:bg-gray-700 transition-colors">
                    <Wand2 size={14} />
                  </button>
                  <div className="flex items-center rounded-md overflow-hidden bg-blue-500 hover:bg-blue-600 text-white font-medium shadow-sm cursor-pointer transition-colors">
                    <button 
                      onClick={() => onSubmitPermissionResponse && onSubmitPermissionResponse(pr.requestId, true)}
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
                    onClick={() => onSubmitPermissionResponse && onSubmitPermissionResponse(pr.requestId, false, "Denied by user")}
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
                <span className="text-[11px] text-text-tertiary truncate">{summary}</span>
              </div>
            </div>
          );
        }
        return <div className="text-[10px] text-text-tertiary italic mt-1 font-mono">Edited {fileAction.targetFile || fileAction.path}</div>;
      case 'runCommand':
        const cmdAction = msg.action.value as any;
        return (
          <div className="mt-3 border border-border-primary rounded-xl overflow-hidden shadow-sm max-w-full">
            <div className="flex items-center justify-between px-3 py-2 text-xs text-text-secondary bg-bg-primary border-b border-border-primary font-medium">
              <div className="flex items-center gap-2">
                <Square size={12} className="text-text-tertiary" />
                <span className="truncate">Command {cmdAction.commandLine?.split(' ')[0]} in {cmdAction.cwd || '~'}</span>
              </div>
              <button className="text-text-tertiary hover:text-text-primary transition-colors"><Copy size={12} /></button>
            </div>
            <div className="p-4 bg-bg-secondary text-[11px] font-mono overflow-x-auto relative">
              <div className="flex items-center gap-2 mb-2 text-blue-400 font-semibold">
                <div className="w-2 h-2 rounded-full bg-blue-500"></div>
                {cmdAction.commandLine}
              </div>
              <div className="text-text-secondary whitespace-pre-wrap">
                {/* Command output typically not present in StepUpdate action, 
                    but if it is streamed back, it would be in msg.text, which is rendered outside. 
                    If it's a finished step, we might just show a placeholder or truncate. */}
                {msg.state === StepUpdate_State.ACTIVE ? <span className="animate-pulse">Running...</span> : "Executed successfully"}
              </div>
              <div className="absolute bottom-2 right-3 text-text-tertiary bg-bg-secondary p-1 rounded-full shadow border border-border-primary">
                <ChevronDown size={14} />
              </div>
            </div>
          </div>
        );
      case 'viewFile':
        const readAction = msg.action.value as any;
        const readFileName = readAction.targetFile?.split('/').pop() || readAction.path?.split('/').pop() || "file";
        return (
          <div className="flex items-center gap-2 mt-2 text-xs text-text-secondary">
            <FileCode size={14} className="text-text-tertiary" />
            <span>Read <span className="text-blue-400 cursor-pointer hover:underline">{readFileName}</span></span>
          </div>
        );
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
          
          if (isUser) {
            return (
              <motion.div 
                key={msg.stepIndex}
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                className="max-w-[85%] self-end flex flex-col gap-1 mt-4"
              >
                <div className="bg-blue-600 text-white px-4 py-2.5 rounded-2xl rounded-tr-sm text-[13px] shadow-sm whitespace-pre-wrap leading-relaxed">
                  {msg.text}
                </div>
              </motion.div>
            );
          }

          return (
            <motion.div 
              key={msg.stepIndex}
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              className="max-w-[95%] self-start flex flex-col gap-2 mt-6 w-full"
            >
              {msg.thinking && (
                <details className="mt-1 mb-2 group">
                  <summary className="text-xs font-semibold text-text-secondary cursor-pointer hover:text-text-primary transition-colors select-none flex items-center gap-2">
                    <Brain size={14} className="text-text-tertiary" />
                    Thoughts
                  </summary>
                  <div className="mt-2 ml-5 text-[12px] text-text-secondary bg-bg-primary/30 p-3 rounded-lg border border-border-primary/50 whitespace-pre-wrap font-mono leading-relaxed max-h-[300px] overflow-y-auto scrollbar-thin">
                    {msg.thinking}
                  </div>
                </details>
              )}
              
              {hasText && <div className="whitespace-pre-wrap leading-relaxed text-[13px] text-text-primary">{msg.text}</div>}
              
              {renderAction(msg)}
              
              <div className="flex items-center justify-between mt-3 text-text-tertiary">
                <div className="flex items-center gap-3">
                  <button className="hover:text-text-secondary transition-colors"><ThumbsUp size={14} /></button>
                  <button className="hover:text-text-secondary transition-colors"><ThumbsDown size={14} /></button>
                </div>
                <div className="flex items-center gap-3">
                  <button className="hover:text-text-secondary transition-colors"><Copy size={14} /></button>
                  <button className="hover:text-text-secondary transition-colors"><GitFork size={14} /></button>
                  <button className="hover:text-text-secondary transition-colors"><BarChart2 size={14} /></button>
                  <button className="hover:text-text-secondary transition-colors"><MoreHorizontal size={14} /></button>
                </div>
              </div>
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
      
      <div className="p-4 relative mt-auto">
        <div className="flex flex-col bg-bg-primary rounded-xl border border-border-primary focus-within:border-blue-500/50 transition-colors shadow-sm mb-2">
          <textarea 
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                handleSend();
              }
            }}
            disabled={!connected || !!connectionError}
            placeholder={connected ? "Tip: Drag a session in the sidebar into a space to group it" : connectionError ? "Cannot send messages due to error" : "Connecting..."}
            className={`w-full bg-transparent border-none outline-none text-[13px] text-text-primary placeholder:text-text-tertiary p-3 min-h-[60px] resize-none ${connected ? '' : 'opacity-50'}`}
          />
          <div className="flex items-center justify-between px-3 py-2 border-t border-border-primary/30">
            <div className="flex items-center gap-1.5">
              <button className="p-1 hover:bg-bg-tertiary rounded-md text-text-tertiary hover:text-text-primary transition-colors">
                <Plus size={16} />
              </button>
              <div className="flex items-center gap-1.5 text-green-500 font-medium text-xs px-2 py-1 rounded-md hover:bg-bg-tertiary cursor-pointer transition-colors">
                <MessageCircle size={14} />
                Ask
              </div>
              <div className="flex items-center text-xs text-text-secondary px-2 py-1 rounded-md hover:bg-bg-tertiary cursor-pointer transition-colors font-medium">
                SWE-1.6 Slow
              </div>
            </div>
            <div className="flex items-center gap-3">
              <div className="flex items-center gap-1 text-xs text-text-tertiary font-medium">
                <Server size={14} />
                Devin Local
              </div>
              <button className="text-text-tertiary hover:text-text-primary transition-colors">
                <Mic size={16} />
              </button>
              <button 
                onClick={handleSend}
                disabled={!connected || !input.trim() || !!connectionError}
                className="w-7 h-7 flex items-center justify-center bg-gray-500 hover:bg-gray-400 text-white rounded-full transition-colors disabled:opacity-30 disabled:hover:bg-gray-500"
              >
                <ArrowUp size={14} strokeWidth={3} />
              </button>
            </div>
          </div>
        </div>
        <div className="flex items-center gap-4 text-[11px] text-text-tertiary px-1 font-medium">
          <span className="flex items-center gap-1.5 hover:text-text-secondary cursor-pointer transition-colors"><Monitor size={12} /> Local</span>
          <span className="flex items-center gap-1.5 hover:text-text-secondary cursor-pointer transition-colors"><Folder size={12} /> otel-aws-log-parser</span>
        </div>
      </div>
    </div>
  );
}
