import { useEffect, useRef, useState } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { SplitSquareHorizontal, Trash2, X } from 'lucide-react';
import { StepUpdate } from '../gen/localharness/v1/localharness_pb';
import '@xterm/xterm/css/xterm.css';
import { listen, UnlistenFn } from '@tauri-apps/api/event';
import { invoke } from '@tauri-apps/api/core';

interface TerminalPanelProps {
  steps?: StepUpdate[];
}

export function TerminalPanel({ steps = [] }: TerminalPanelProps) {
  const terminalRef = useRef<HTMLDivElement>(null);
  const outputRef = useRef<HTMLDivElement>(null);
  const ptyTermInstanceRef = useRef<Terminal | null>(null);
  const outputTermInstanceRef = useRef<Terminal | null>(null);
  const ptyFitAddonRef = useRef<FitAddon | null>(null);
  const outputFitAddonRef = useRef<FitAddon | null>(null);
  const [activeTab, setActiveTab] = useState<'problems' | 'output' | 'debug' | 'terminal' | 'ports'>('terminal');

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

  // Initialize Output Terminal
  useEffect(() => {
    if (!outputRef.current) return;
    if (outputTermInstanceRef.current) return;
    
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
    
    term.open(outputRef.current);
    outputTermInstanceRef.current = term;
    outputFitAddonRef.current = fitAddon;
    
    setTimeout(() => fitAddon.fit(), 50);
    
    term.writeln('\x1b[1;34m$ \x1b[0m local-harness sidecar logs & agent output ready.');
    
    const resizeObserver = new ResizeObserver(() => {
      requestAnimationFrame(() => {
         if (outputRef.current && outputRef.current.clientWidth > 0) {
            fitAddon.fit();
         }
      });
    });
    resizeObserver.observe(outputRef.current);
    
    let unlistenFn: UnlistenFn | null = null;
    listen<string>('sidecar-log', (event) => {
      term.writeln(`\x1b[36m[Sidecar]\x1b[0m ${event.payload}`);
    }).then(unlisten => {
      unlistenFn = unlisten;
    });

    return () => {
      if (unlistenFn) unlistenFn();
      resizeObserver.disconnect();
      term.dispose();
      outputTermInstanceRef.current = null;
    };
  }, []);

  // Refit when tabs change
  useEffect(() => {
    setTimeout(() => {
      if (activeTab === 'terminal' && ptyFitAddonRef.current) ptyFitAddonRef.current.fit();
      if (activeTab === 'output' && outputFitAddonRef.current) outputFitAddonRef.current.fit();
    }, 10);
  }, [activeTab]);

  const processedLengths = useRef<Record<number, { stdout: number, stderr: number }>>({});

  // Handle incoming steps to write commands and output
  useEffect(() => {
    if (!outputTermInstanceRef.current || steps.length === 0) return;
    const term = outputTermInstanceRef.current;

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

  return (
    <div className="h-full flex flex-col bg-bg-primary border-t border-border-primary overflow-hidden">
      <div className="h-9 flex items-center justify-between px-4 text-xs bg-bg-secondary border-b border-border-primary">
        <div className="flex gap-4 h-full">
          {(['problems', 'output', 'debug', 'terminal', 'ports'] as const).map((tab) => (
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
              {tab === 'debug' ? 'Debug Console' : tab}
            </button>
          ))}
        </div>
        <div className="flex items-center gap-3 text-text-tertiary">
          <button className="hover:text-text-primary"><SplitSquareHorizontal size={14} /></button>
          <button className="hover:text-text-primary"><Trash2 size={14} /></button>
          <button className="hover:text-text-primary"><X size={14} /></button>
        </div>
      </div>

      <div className="flex-1 relative">
        <div className="absolute inset-0 p-2 overflow-hidden" style={{ display: activeTab === 'terminal' ? 'block' : 'none' }} ref={terminalRef} />
        <div className="absolute inset-0 p-2 overflow-hidden" style={{ display: activeTab === 'output' ? 'block' : 'none' }} ref={outputRef} />
        
        {activeTab !== 'terminal' && activeTab !== 'output' && (
          <div className="absolute inset-0 flex items-center justify-center text-text-tertiary text-sm">
            No {activeTab} available.
          </div>
        )}
      </div>
    </div>
  );
}
