import { useEffect, useRef, useState } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { SplitSquareHorizontal, Trash2, X } from 'lucide-react';
import { StepUpdate } from '../gen/localharness/v1/localharness_pb';
import '@xterm/xterm/css/xterm.css';

interface TerminalPanelProps {
  steps?: StepUpdate[];
}

export function TerminalPanel({ steps = [] }: TerminalPanelProps) {
  const terminalRef = useRef<HTMLDivElement>(null);
  const termInstanceRef = useRef<Terminal | null>(null);
  const [activeTab, setActiveTab] = useState<'problems' | 'output' | 'debug' | 'terminal' | 'ports'>('terminal');

  useEffect(() => {
    if (!terminalRef.current || activeTab !== 'terminal') return;
    
    const term = new Terminal({
      theme: { 
        background: '#11111b', 
        foreground: '#cdd6f4',
        cursor: '#f5e0dc',
        selectionBackground: '#585b70'
      },
      fontFamily: "'JetBrains Mono', 'Fira Code', monospace",
      fontSize: 13,
      cursorBlink: true,
      convertEol: true, // Important for properly rendering \n as \r\n in xterm
    });
    
    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    
    term.open(terminalRef.current);
    termInstanceRef.current = term;
    
    // Slight delay to ensure DOM is ready for fit
    setTimeout(() => fitAddon.fit(), 10);
    
    term.writeln('\x1b[1;34m$ \x1b[0m local-harness sidecar terminal ready.');
    term.writeln('\x1b[1;34m$ \x1b[0m waiting for connection...');
    
    const resizeObserver = new ResizeObserver(() => {
      // Debounce fit slightly
      requestAnimationFrame(() => fitAddon.fit());
    });
    resizeObserver.observe(terminalRef.current);
    
    return () => {
      resizeObserver.disconnect();
      term.dispose();
      termInstanceRef.current = null;
    };
  }, [activeTab]);

  const processedLengths = useRef<Record<number, { stdout: number, stderr: number }>>({});

  // Handle incoming steps to write commands and output
  useEffect(() => {
    if (!termInstanceRef.current || steps.length === 0) return;
    const term = termInstanceRef.current;

    for (let i = 0; i < steps.length; i++) {
      const step = steps[i];
      
      // We only care about tool executions
      if (!step.action.case) continue;
      
      if (step.action.case === 'runCommand') {
        const cmd = step.action.value;
        const state = processedLengths.current[step.stepIndex] || { stdout: 0, stderr: 0 };
        
        // First time seeing this command? Write the prompt.
        if (state.stdout === 0 && state.stderr === 0 && cmd.command) {
          term.writeln(`\r\n\x1b[1;32m$ ${cmd.command}\x1b[0m`);
        }
        
        // Stream new stdout
        if (cmd.stdout && cmd.stdout.length > state.stdout) {
          const delta = cmd.stdout.substring(state.stdout);
          term.write(delta);
          state.stdout = cmd.stdout.length;
        }
        
        // Stream new stderr (with red coloring)
        if (cmd.stderr && cmd.stderr.length > state.stderr) {
          const delta = cmd.stderr.substring(state.stderr);
          term.write(`\x1b[31m${delta}\x1b[0m`);
          state.stderr = cmd.stderr.length;
        }
        
        processedLengths.current[step.stepIndex] = state;
      }
    }
  }, [steps]);

  return (
    <div className="h-full flex flex-col bg-[#11111b] border-t border-[#313244] overflow-hidden">
      <div className="h-9 flex items-center justify-between px-4 text-xs bg-[#181825] border-b border-[#313244]">
        <div className="flex gap-4 h-full">
          {(['problems', 'output', 'debug', 'terminal', 'ports'] as const).map((tab) => (
            <button
              key={tab}
              onClick={() => setActiveTab(tab)}
              className={`h-full uppercase tracking-wider font-semibold transition-colors border-b-2 ${
                activeTab === tab 
                  ? 'text-[#cdd6f4] border-blue-400' 
                  : 'text-[#6c7086] border-transparent hover:text-[#a6adc8]'
              }`}
            >
              {tab === 'debug' ? 'Debug Console' : tab}
            </button>
          ))}
        </div>
        <div className="flex items-center gap-3 text-[#6c7086]">
          <button className="hover:text-white"><SplitSquareHorizontal size={14} /></button>
          <button className="hover:text-white"><Trash2 size={14} /></button>
          <button className="hover:text-white"><X size={14} /></button>
        </div>
      </div>
      
      <div className="flex-1 relative">
        {activeTab === 'terminal' ? (
          <div className="absolute inset-0 p-2 overflow-hidden" ref={terminalRef} />
        ) : (
          <div className="absolute inset-0 flex items-center justify-center text-[#7f849c] text-sm">
            No {activeTab} available.
          </div>
        )}
      </div>
    </div>
  );
}
