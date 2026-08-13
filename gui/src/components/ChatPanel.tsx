import { useState } from 'react';
import { Send, TerminalSquare } from 'lucide-react';
import { motion } from 'framer-motion';

export function ChatPanel() {
  const [input, setInput] = useState('');

  return (
    <div className="flex flex-col h-full bg-[#181825] text-[#cdd6f4]">
      <div className="p-4 border-b border-[#313244] font-semibold flex items-center gap-2">
        <TerminalSquare size={18} className="text-blue-400" />
        LocalHarness
      </div>
      <div className="flex-1 overflow-y-auto p-4 space-y-4 flex flex-col">
        {/* Placeholder for chat messages */}
        <motion.div 
          initial={{ opacity: 0, y: 10 }}
          animate={{ opacity: 1, y: 0 }}
          className="bg-[#313244] p-3 rounded-lg max-w-[85%] self-start text-sm shadow-md"
        >
          Hello! I am your local AI agent. How can I help you today?
        </motion.div>
      </div>
      <div className="p-4 border-t border-[#313244]">
        <div className="flex items-center bg-[#1e1e2e] rounded-md border border-[#313244] focus-within:border-blue-500 transition-colors px-3 py-2 shadow-inner">
          <input 
            className="flex-1 bg-transparent outline-none text-sm placeholder:text-[#585b70]" 
            placeholder="Ask anything..." 
            value={input}
            onChange={(e) => setInput(e.target.value)}
          />
          <button className="text-blue-400 hover:text-blue-300 ml-2 p-1 rounded hover:bg-[#313244] transition-colors">
            <Send size={16} />
          </button>
        </div>
      </div>
    </div>
  );
}
