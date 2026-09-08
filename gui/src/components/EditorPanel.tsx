import { useState, useMemo } from 'react';
import Editor from '@monaco-editor/react';
import { Code2, Globe, FileText, ArrowLeft, ArrowRight, RotateCw, Home, FolderOpen, GitBranch, Terminal as TerminalIcon } from 'lucide-react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { StepUpdate } from '../gen/localharness/v1/localharness_pb';

interface EditorPanelProps {
  steps?: StepUpdate[];
  userActiveFile?: string | null;
  userActiveContent?: string;
}

export function EditorPanel({ steps = [], userActiveFile = null, userActiveContent = "" }: EditorPanelProps) {
  const [activeTab, setActiveTab] = useState<'editor' | 'browser' | 'planner'>('editor');

  const { filePath, fileContent, browserUrl, plannerContent, isEmpty } = useMemo(() => {
    let recentFile = 'Welcome.md';
    let recentFileContent = '';
    let recentBrowserUrl = 'https://google.com';
    let recentPlannerContent = '# No plans yet\n\nWhen the agent creates a task list or implementation plan, it will appear here.';
    let hasActions = false;

    // Traverse steps backwards to find the most recent actions for each tab type
    for (let i = steps.length - 1; i >= 0; i--) {
      const action = steps[i].action;
      if (!action.case) continue;
      hasActions = true;

      // 1. Check for File edits (Editor Tab)
      if (action.case === 'writeToFile' || action.case === 'replaceFileContent' || action.case === 'viewFile') {
        const path = action.value.path;
        if (recentFile === 'Welcome.md') {
          recentFile = path;
          recentFileContent = action.case === 'replaceFileContent' 
            ? "// File modified via replaceFileContent..." 
            : (action.value.content || "// File viewed...");
        }

        // 2. Check for Artifacts / Plans (Planner Tab)
        if (path.endsWith('task.md') || path.endsWith('implementation_plan.md') || path.endsWith('walkthrough.md')) {
           if (recentPlannerContent.startsWith('# No plans yet')) {
             recentPlannerContent = action.case === 'replaceFileContent'
               ? "> *File modified, waiting for full sync...*"
               : (action.value.content || "");
           }
        }
      }

      // 3. Check for Browser actions (Browser Tab)
      if (action.case === 'readUrlContent') {
        if (recentBrowserUrl === 'https://google.com') {
          recentBrowserUrl = action.value.url;
        }
      }
    }
    
    return { 
      filePath: userActiveFile || recentFile, 
      fileContent: userActiveContent || recentFileContent,
      browserUrl: recentBrowserUrl,
      plannerContent: recentPlannerContent,
      isEmpty: !hasActions && !userActiveFile && recentFile === 'Welcome.md'
    };
  }, [steps, userActiveFile, userActiveContent]);

  // If empty, render the Dashboard
  if (isEmpty) {
    return (
      <div className="h-full w-full bg-bg-secondary flex flex-col items-center justify-center p-8 text-text-primary">
        <div className="w-full max-w-3xl flex flex-col items-center gap-12">

          <div className="flex justify-center opacity-80">
            <svg width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1" strokeLinecap="round" strokeLinejoin="round" className="text-text-tertiary" style={{ color: 'var(--text-tertiary)' }}>
              <path d="m12 3-1.912 5.813a2 2 0 0 1-1.275 1.275L3 12l5.813 1.912a2 2 0 0 1 1.275 1.275L12 21l1.912-5.813a2 2 0 0 1 1.275-1.275L21 12l-5.813-1.912a2 2 0 0 1-1.275-1.275L12 3Z"/>
              <path d="M5 3v4M3 5h4"/>
            </svg>
          </div>

          <div className="w-full max-w-2xl bg-bg-tertiary rounded-xl border border-border-primary shadow-lg p-2 flex flex-col">
            <div className="text-xs text-text-tertiary px-4 pt-2 pb-1">Tip: Type a command to start a session</div>
            <div className="flex items-center gap-2 px-4 py-3">
              <span className="text-accent-primary font-bold" style={{ color: 'var(--accent-primary)' }}>+</span>
              <span className="text-sm font-mono text-text-tertiary">&lt;/&gt; Code</span>
              <span className="text-sm font-mono text-text-tertiary">Agent-1.0</span>
              <div className="flex-1" />
              <span className="text-xs text-text-tertiary flex items-center gap-1"><TerminalIcon size={12}/> Local Engine</span>
            </div>
            <div className="border-t border-border-primary px-4 py-3 flex items-center gap-4 text-xs text-text-tertiary">
              <span className="flex items-center gap-1 hover:text-text-primary cursor-pointer"><FolderOpen size={14}/> Open directory...</span>
            </div>
          </div>

          <div className="w-full max-w-2xl grid grid-cols-3 gap-4">
            <button className="flex flex-col gap-2 p-4 bg-bg-tertiary rounded-lg border border-border-primary hover:border-accent-primary transition-colors text-left group">
              <FolderOpen size={18} className="text-text-tertiary group-hover:text-accent-primary" style={{ '--tw-text-opacity': '1' } as any}/>
              <span className="text-sm font-medium">Open project</span>
            </button>
            <button className="flex flex-col gap-2 p-4 bg-bg-tertiary rounded-lg border border-border-primary hover:border-accent-primary transition-colors text-left group">
              <GitBranch size={18} className="text-text-tertiary group-hover:text-accent-primary" style={{ '--tw-text-opacity': '1' } as any}/>
              <span className="text-sm font-medium">Clone repository</span>
            </button>
            <button className="flex flex-col gap-2 p-4 bg-bg-tertiary rounded-lg border border-border-primary hover:border-accent-primary transition-colors text-left group">
              <TerminalIcon size={18} className="text-text-tertiary group-hover:text-accent-primary" style={{ '--tw-text-opacity': '1' } as any}/>
              <span className="text-sm font-medium">Connect via SSH</span>
            </button>
          </div>

          <div className="w-full max-w-2xl bg-bg-tertiary rounded-lg border border-border-primary overflow-hidden">
            <div className="px-4 py-3 text-xs font-semibold text-text-tertiary uppercase tracking-wider border-b border-border-primary">Start new session</div>
            <button className="w-full text-left px-4 py-3 text-sm hover:bg-bg-tertiary/50 transition-colors">Explore my codebase and diagram how it works</button>
            <button className="w-full text-left px-4 py-3 text-sm hover:bg-bg-tertiary/50 transition-colors border-t border-border-primary">Review my latest changes and suggest improvements</button>
            <button className="w-full text-left px-4 py-3 text-sm hover:bg-bg-tertiary/50 transition-colors border-t border-border-primary">Write tests for my most critical code paths</button>
          </div>

        </div>
      </div>
    );
  }

  return (
    <div className="h-full w-full bg-bg-tertiary flex flex-col">
      <div className="flex items-end px-2 pt-2 bg-bg-primary border-b border-border-primary gap-1 shrink-0">
        <button
          onClick={() => setActiveTab('editor')}
          className={`flex items-center gap-2 px-4 py-2 text-sm rounded-t-lg transition-colors ${activeTab === 'editor' ? 'bg-bg-tertiary text-accent-primary border-t border-l border-r border-border-primary' : 'text-text-tertiary hover:bg-bg-tertiary/50'}`}
          style={activeTab === 'editor' ? { color: 'var(--accent-primary)' } : {}}
        >
          <Code2 size={16} /> Editor
        </button>
        <button
          onClick={() => setActiveTab('browser')}
          className={`flex items-center gap-2 px-4 py-2 text-sm rounded-t-lg transition-colors ${activeTab === 'browser' ? 'bg-bg-tertiary text-accent-primary border-t border-l border-r border-border-primary' : 'text-text-tertiary hover:bg-bg-tertiary/50'}`}
          style={activeTab === 'browser' ? { color: 'var(--accent-primary)' } : {}}
        >
          <Globe size={16} /> Browser
        </button>
        <button
          onClick={() => setActiveTab('planner')}
          className={`flex items-center gap-2 px-4 py-2 text-sm rounded-t-lg transition-colors ${activeTab === 'planner' ? 'bg-bg-tertiary text-accent-primary border-t border-l border-r border-border-primary' : 'text-text-tertiary hover:bg-bg-tertiary/50'}`}
          style={activeTab === 'planner' ? { color: 'var(--accent-primary)' } : {}}
        >
          <FileText size={16} /> Planner
        </button>
      </div>

      <div className="flex-1 relative bg-bg-tertiary overflow-hidden">
        {activeTab === 'editor' && (
          <div className="absolute inset-0 flex flex-col">
            <div className="px-4 py-2 text-xs font-mono text-text-tertiary border-b border-border-primary bg-bg-tertiary/50 flex items-center shrink-0">
              {filePath}
            </div>
            <Editor 
              height="100%" 
              language={filePath.endsWith('.ts') || filePath.endsWith('.tsx') ? 'typescript' : 
                        filePath.endsWith('.js') || filePath.endsWith('.jsx') ? 'javascript' :
                        filePath.endsWith('.json') ? 'json' :
                        filePath.endsWith('.md') ? 'markdown' :
                        filePath.endsWith('.go') ? 'go' :
                        filePath.endsWith('.rs') ? 'rust' : 'plaintext'} 
              theme="vs-dark"
              value={fileContent}
              options={{ 
                minimap: { enabled: false }, 
                fontSize: 14,
                fontFamily: "'JetBrains Mono', 'Fira Code', monospace",
                padding: { top: 16 },
                readOnly: true
              }}
            />
          </div>
        )}
        {activeTab === 'browser' && (
          <div className="absolute inset-0 flex flex-col bg-bg-primary">
            <div className="flex items-center gap-2 p-2 bg-bg-secondary border-b border-border-primary shrink-0 text-text-secondary">
              <button className="p-1 hover:bg-bg-secondary rounded"><ArrowLeft size={16} /></button>
              <button className="p-1 hover:bg-bg-secondary rounded"><ArrowRight size={16} /></button>
              <button className="p-1 hover:bg-bg-secondary rounded"><RotateCw size={16} /></button>
              <button className="p-1 hover:bg-bg-secondary rounded mr-2"><Home size={16} /></button>
              <div className="flex-1 bg-bg-primary rounded-full px-4 py-1 text-sm border border-border-primary shadow-inner overflow-hidden text-ellipsis whitespace-nowrap">
                {browserUrl}
              </div>
            </div>
            <div className="flex-1 bg-bg-primary flex items-center justify-center">
              <iframe 
                src={browserUrl} 
                className="w-full h-full border-0"
                title="Browser View"
                sandbox="allow-scripts allow-same-origin"
                onError={(e) => console.log('Iframe load error, likely x-frame-options', e)}
              />
            </div>
          </div>
        )}
        {activeTab === 'planner' && (
          <div className="absolute inset-0 overflow-y-auto p-8 text-text-primary prose prose-invert max-w-none">
            <ReactMarkdown remarkPlugins={[remarkGfm]}>
              {plannerContent}
            </ReactMarkdown>
          </div>
        )}
      </div>
    </div>
  );
}
