import { useState, useMemo } from 'react';
import { Send, TerminalSquare, User, Bot } from 'lucide-react';
import { motion } from 'framer-motion';
import { StepUpdate, StepUpdate_Source } from '../gen/localharness/v1/localharness_pb';

interface ChatPanelProps {
  connected: boolean;
  steps: StepUpdate[];
  onSend: (text: string) => void;
}

export function ChatPanel({ connected, steps, onSend }: ChatPanelProps) {
  const [input, setInput] = useState('');

  // Group streaming steps by stepIndex so we have one cohesive message per step
  const messages = useMemo(() => {
    const grouped = new Map<number, StepUpdate>();
    
    for (const step of steps) {
      if (!grouped.has(step.stepIndex)) {
        grouped.set(step.stepIndex, step);
      } else {
        const existing = grouped.get(step.stepIndex)!;
        // Merge text if it's a streaming update
        if (step.textDelta) {
          existing.text += step.textDelta;
        } else if (step.text) {
          existing.text = step.text;
        }
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

  return (
    <div className="flex flex-col h-full bg-[#181825] text-[#cdd6f4]">
      <div className="p-4 border-b border-[#313244] font-semibold flex items-center justify-between">
        <div className="flex items-center gap-2">
          <TerminalSquare size={18} className="text-blue-400" />
          LocalHarness
        </div>
        <div className="flex items-center gap-2 text-xs">
          <div className={`w-2 h-2 rounded-full ${connected ? 'bg-green-500' : 'bg-red-500'}`} />
          {connected ? 'Connected' : 'Disconnected'}
        </div>
      </div>
      
      <div className="flex-1 overflow-y-auto p-4 space-y-4 flex flex-col">
        {messages.length === 0 && (
          <div className="text-center text-[#585b70] mt-10 text-sm">
            Ready to chat. Ask me anything!
          </div>
        )}
        
        {messages.map((msg) => {
          const isUser = msg.source === StepUpdate_Source.USER;
          
          return (
            <motion.div 
              key={msg.stepIndex}
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              className={`p-3 rounded-lg max-w-[90%] text-sm shadow-md flex flex-col gap-1 ${
                isUser 
                  ? 'bg-blue-600/20 text-blue-100 self-end border border-blue-500/20' 
                  : 'bg-[#313244] self-start'
              }`}
            >
              <div className="flex items-center gap-2 mb-1 text-xs opacity-70">
                {isUser ? <User size={12} /> : <Bot size={12} />}
                {isUser ? 'You' : 'Agent'}
              </div>
              <div className="whitespace-pre-wrap">{msg.text || (msg.action?.case ? `[Tool Execution: ${msg.action.case}]` : '...')}</div>
            </motion.div>
          );
        })}
      </div>
      
      <div className="p-4 border-t border-[#313244]">
        <div className="flex items-center bg-[#1e1e2e] rounded-md border border-[#313244] focus-within:border-blue-500 transition-colors px-3 py-2 shadow-inner">
          <input 
            className="flex-1 bg-transparent outline-none text-sm placeholder:text-[#585b70] disabled:opacity-50" 
            placeholder={connected ? "Ask anything..." : "Connecting..."}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                handleSend();
              }
            }}
            disabled={!connected}
          />
          <button 
            onClick={handleSend}
            disabled={!connected || !input.trim()}
            className="text-blue-400 hover:text-blue-300 ml-2 p-1 rounded hover:bg-[#313244] transition-colors disabled:opacity-50 disabled:hover:bg-transparent"
          >
            <Send size={16} />
          </button>
        </div>
      </div>
    </div>
  );
}
