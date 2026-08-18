import { useEffect, useRef, useState } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { SplitSquareHorizontal, Trash2, X, ChevronDown } from 'lucide-react';
import { StepUpdate } from '../gen/localharness/v1/localharness_pb';
import '@xterm/xterm/css/xterm.css';
import { listen, UnlistenFn } from '@tauri-apps/api/event';
import { invoke } from '@tauri-apps/api/core';

interface TerminalPanelProps {
  steps?: StepUpdate[];
  activeSessionId?: string | null;
}

type OutputSourceType = 'agent' | 'rust' | 'go' | 'react';

interface OutputSource {
  id: string;
  type: OutputSourceType;
  label: string;
}

interface SidecarLogEvent {
  session_id: string;
  log: string;
}

interface RustLogEvent {
  log: string;
}

export function TerminalPanel({ steps = [], activeSessionId }: TerminalPanelProps) {
  const terminalRef = useRef<HTMLDivElement>(null);
  
  const outputContainerRef = useRef<HTMLDivElement>(null);
  const outputTerminalsRef = useRef<Map<string, Terminal>>(new Map());
  const outputFitAddonsRef = useRef<Map<string, FitAddon>>(new Map());

  const ptyTermInstanceRef = useRef<Terminal | null>(null);
  const ptyFitAddonRef = useRef<FitAddon | null>(null);
  const [activeTab, setActiveTab] = useState<'problems' | 'output' | 'terminal' | 'ports'>('terminal');

  const [outputSources, setOutputSources] = useState<OutputSource[]>([
    { id: 'agent', type: 'agent', label: 'Agent Commands' },
    { id: 'rust', type: 'rust', label: 'Tauri Backend (Rust)' },
    { id: 'react', type: 'react', label: 'React App (Frontend)' },
  ]);
  const [activeOutputId, setActiveOutputId] = useState<string>('agent');

  // Used to prevent mixing lengths between sessions
  const currentSessionIdRef = useRef<string | null>(null);
  const processedLengths = useRef<Record<number, { stdout: number, stderr: number }>>({});

  useEffect(() => {
    if (activeSessionId !== currentSessionIdRef.current) {
      processedLengths.current = {};
      currentSessionIdRef.current = activeSessionId || null;
      
      const term = outputTerminalsRef.current.get('agent');
      if (term) {
        term.clear();
        term.writeln('\x1b[1;34m$ \x1b[0m local-harness agent output ready.');
      }
    }
  }, [activeSessionId]);

  useEffect(() => {
    const originalConsoleLog = console.log;
    const originalConsoleWarn = console.warn;
    const originalConsoleError = console.error;

    const logToTerminal = (level: string, ...args: any[]) => {
      const term = outputTerminalsRef.current.get('react');
      if (term) {
        const msg = args.map(a => {
          if (a instanceof Error) return a.stack || a.message;
          return typeof a === 'object' ? JSON.stringify(a) : String(a);
        }).join(' ');
        
        let color = '\x1b[0m'; // default
        if (level === 'WARN') color = '\x1b[33m'; // yellow
        if (level === 'ERROR') color = '\x1b[31m'; // red
        
        term.writeln(`${color}[${level}]\x1b[0m ${msg}`);
      }
    };

    console.log = (...args) => {
      originalConsoleLog(...args);
      logToTerminal('INFO', ...args);
    };
    console.warn = (...args) => {
      originalConsoleWarn(...args);
      logToTerminal('WARN', ...args);
    };
    console.error = (...args) => {
      originalConsoleError(...args);
      logToTerminal('ERROR', ...args);
    };

    return () => {
      console.log = originalConsoleLog;
      console.warn = originalConsoleWarn;
      console.error = originalConsoleError;
    };
  }, []);

  const initOutputTerminal = (id: string, el: HTMLDivElement | null) => {
    if (!el || outputTerminalsRef.current.has(id)) return;
    
    const term = new Terminal({
      theme: {
        background: 'var(--bg-primary, #000000)',
        foreground: 'var(--text-primary, #F9FAFB)',
        cursor: 'var(--accent-primary, #f5e0dc)',
        selectionBackground: 'var(--border-highlight, #585b70)'
      },
      fontFamily: "'JetBrains Mono', 'Fira Code', monospace",
      fontSize: 13,
      cursorBlink: true,
      convertEol: true,
    });
    
    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(el);
    
    outputTerminalsRef.current.set(id, term);
    outputFitAddonsRef.current.set(id, fitAddon);
    
    setTimeout(() => fitAddon.fit(), 50);

    if (id === 'agent') {
      term.writeln('\x1b[1;34m$ \x1b[0m local-harness agent output ready.');
    } else if (id === 'rust') {
      term.writeln('\x1b[1;34m$ \x1b[0m Tauri Backend (Rust) log capture active.');
    } else if (id === 'react') {
      term.writeln('\x1b[1;34m$ \x1b[0m React App Console captured.');
    } else {
      term.writeln(`\x1b[1;34m$ \x1b[0m Local Harness (Go) connected.`);
    }
  };

  // Initialize PTY Terminal
  useEffect(() => {
    if (!terminalRef.current) return;
    if (ptyTermInstanceRef.current) return;
    
    const term = new Terminal({
      theme: {
        background: 'var(--bg-primary, #000000)',
        foreground: 'var(--text-primary, #F9FAFB)',
        cursor: 'var(--accent-primary, #f5e0dc)',
        selectionBackground: 'var(--border-highlight, #585b70)'
      },
      fontFamily: "'JetBrains Mono', 'Fira Code', monospace",
      fontSize: 13,
      cursorBlink: true,
      convertEol: true,
    });
    
    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    
    term.open(terminalRef.current);
    ptyTermInstanceRef.current = term;
    ptyFitAddonRef.current = fitAddon;
    
    setTimeout(() => {
      fitAddon.fit();
      invoke('spawn_pty', { rows: term.rows, cols: term.cols }).catch(console.error);
    }, 50);

    let unlistenFn: UnlistenFn | null = null;
    listen<number[]>('pty-output', (event) => {
      term.write(new Uint8Array(event.payload));
    }).then(unlisten => {
      unlistenFn = unlisten;
    });

    const onDataDisposable = term.onData((data) => {
      invoke('write_pty', { data }).catch(console.error);
    });

    const onResizeDisposable = term.onResize((size) => {
      invoke('resize_pty', { rows: size.rows, cols: size.cols }).catch(console.error);
    });

    const resizeObserver = new ResizeObserver(() => {
      requestAnimationFrame(() => {
        if (terminalRef.current && terminalRef.current.clientWidth > 0) {
           fitAddon.fit();
        }
      });
    });
    resizeObserver.observe(terminalRef.current);
    
    return () => {
      if (unlistenFn) unlistenFn();
      onDataDisposable.dispose();
      onResizeDisposable.dispose();
      resizeObserver.disconnect();
      term.dispose();
      ptyTermInstanceRef.current = null;
    };
  }, []);

  // Set up global observers and output streams
  useEffect(() => {
    let unlistenSidecar: UnlistenFn | null = null;
    let unlistenRust: UnlistenFn | null = null;

    listen<SidecarLogEvent>('sidecar-log', (event) => {
      const id = `go:${event.payload.session_id}`;
      
      setOutputSources(prev => {
        if (!prev.find(s => s.id === id)) {
           return [...prev, { id, type: 'go', label: `Local Harness - Go (${event.payload.session_id.slice(0,8)})` }];
        }
        return prev;
      });

      setTimeout(() => {
          const term = outputTerminalsRef.current.get(id);
          if (term) {
             term.writeln(`\x1b[36m[Sidecar]\x1b[0m ${event.payload.log}`);
          }
      }, 0);
    }).then(u => unlistenSidecar = u);

    listen<RustLogEvent>('rust-log', (event) => {
      const term = outputTerminalsRef.current.get('rust');
      if (term) {
        term.writeln(`\x1b[32m[Rust]\x1b[0m ${event.payload.log}`);
      }
    }).then(u => unlistenRust = u);

    const resizeObserver = new ResizeObserver(() => {
      requestAnimationFrame(() => {
         if (outputContainerRef.current && outputContainerRef.current.clientWidth > 0) {
            outputFitAddonsRef.current.get(activeOutputId)?.fit();
         }
      });
    });
    if (outputContainerRef.current) {
      resizeObserver.observe(outputContainerRef.current);
    }

    return () => {
      if (unlistenSidecar) unlistenSidecar();
      if (unlistenRust) unlistenRust();
      resizeObserver.disconnect();
      // Dispose all dynamic terminals
      outputTerminalsRef.current.forEach(term => term.dispose());
      outputTerminalsRef.current.clear();
      outputFitAddonsRef.current.clear();
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []); // Only run once on mount

  // Refit when tabs change
  useEffect(() => {
    setTimeout(() => {
      if (activeTab === 'terminal' && ptyFitAddonRef.current) ptyFitAddonRef.current.fit();
      if (activeTab === 'output' && activeOutputId) {
        outputFitAddonsRef.current.get(activeOutputId)?.fit();
      }
    }, 10);
  }, [activeTab, activeOutputId]);

  // Handle incoming steps to write commands and output
  useEffect(() => {
    const term = outputTerminalsRef.current.get('agent');
    if (!term || steps.length === 0) return;

    for (let i = 0; i < steps.length; i++) {
      const step = steps[i];
      if (!step.action.case) continue;
      
      if (step.action.case === 'runCommand') {
        const cmd = step.action.value;
        const state = processedLengths.current[step.stepIndex] || { stdout: 0, stderr: 0 };
        
        if (state.stdout === 0 && state.stderr === 0 && cmd.command) {
          term.writeln(`\r\n\x1b[1;32m$ ${cmd.command}\x1b[0m`);
        }
        
        if (cmd.stdout && cmd.stdout.length > state.stdout) {
          const delta = cmd.stdout.substring(state.stdout);
          term.write(delta);
          state.stdout = cmd.stdout.length;
        }
        
        if (cmd.stderr && cmd.stderr.length > state.stderr) {
          const delta = cmd.stderr.substring(state.stderr);
          term.write(`\x1b[31m${delta}\x1b[0m`);
          state.stderr = cmd.stderr.length;
        }
        
        processedLengths.current[step.stepIndex] = state;
      }
    }
  }, [steps]);

  const handleClearOutput = () => {
    if (activeTab === 'output') {
      const term = outputTerminalsRef.current.get(activeOutputId);
      if (term) term.clear();
    } else if (activeTab === 'terminal') {
      ptyTermInstanceRef.current?.clear();
    }
  };

  return (
    <div className="h-full flex flex-col bg-bg-primary border-t border-border-primary overflow-hidden">
      <div className="h-9 flex items-center justify-between px-4 text-xs bg-bg-secondary border-b border-border-primary">
        <div className="flex gap-4 h-full">
          {(['problems', 'output', 'terminal', 'ports'] as const).map((tab) => (
            <button
              key={tab}
              onClick={() => setActiveTab(tab)}
              className={`h-full uppercase tracking-wider font-semibold transition-colors border-b-2 ${
                activeTab === tab
                  ? 'text-text-primary border-accent-primary'
                  : 'text-text-tertiary border-transparent hover:text-text-secondary'
              }`}
              style={activeTab === tab ? { borderColor: 'var(--accent-primary)' } : {}}
            >
              {tab}
            </button>
          ))}
        </div>
        <div className="flex items-center gap-3 text-text-tertiary">
          {activeTab === 'output' && (
            <div className="relative flex items-center mr-2">
              <select 
                className="bg-bg-primary border border-border-primary text-text-primary text-xs rounded px-2 py-1 appearance-none pr-6 cursor-pointer focus:outline-none focus:border-accent-primary"
                value={activeOutputId}
                onChange={(e) => setActiveOutputId(e.target.value)}
              >
                {outputSources.map(source => (
                  <option key={source.id} value={source.id}>{source.label}</option>
                ))}
              </select>
              <ChevronDown size={12} className="absolute right-2 pointer-events-none text-text-tertiary" />
            </div>
          )}
          <button className="hover:text-text-primary"><SplitSquareHorizontal size={14} /></button>
          <button className="hover:text-text-primary transition-colors" onClick={handleClearOutput} title="Clear Output"><Trash2 size={14} /></button>
          <button className="hover:text-text-primary"><X size={14} /></button>
        </div>
      </div>

      <div className="flex-1 relative bg-bg-primary">
        <div className="absolute inset-0 p-2 overflow-hidden" style={{ display: activeTab === 'terminal' ? 'block' : 'none' }}>
          <div className="w-full h-full" ref={terminalRef} />
        </div>
        
        <div className="absolute inset-0 p-2 overflow-hidden" style={{ display: activeTab === 'output' ? 'block' : 'none' }} ref={outputContainerRef}>
          {outputSources.map(source => (
            <div 
              key={source.id} 
              className="w-full h-full" 
              style={{ display: activeOutputId === source.id ? 'block' : 'none' }}
              ref={(el) => initOutputTerminal(source.id, el)}
            />
          ))}
        </div>

        {activeTab !== 'terminal' && activeTab !== 'output' && (
          <div className="absolute inset-0 flex items-center justify-center text-text-tertiary text-sm">
            No {activeTab} available.
          </div>
        )}
      </div>
    </div>
  );
}
