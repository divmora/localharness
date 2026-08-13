import { useEffect, useRef } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';

export function TerminalPanel() {
  const terminalRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!terminalRef.current) return;
    
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
    });
    
    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    
    term.open(terminalRef.current);
    
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
    };
  }, []);

  return (
    <div className="h-full flex flex-col bg-[#11111b] border-t border-[#313244] overflow-hidden">
      <div className="h-8 flex items-center px-4 text-xs font-semibold text-[#a6adc8] uppercase tracking-wider bg-[#181825] border-b border-[#313244]">
        Terminal
      </div>
      <div className="flex-1 p-2 overflow-hidden" ref={terminalRef} />
    </div>
  );
}
