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
      <div className="h-full w-full bg-[#0A0A0A] flex flex-col items-center justify-center p-8 text-[#F9FAFB]">
        <div className="w-full max-w-3xl flex flex-col items-center gap-12">
          
          <div className="flex justify-center opacity-80">
            <svg width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1" strokeLinecap="round" strokeLinejoin="round" className="text-[#9CA3AF]">
              <path d="m12 3-1.912 5.813a2 2 0 0 1-1.275 1.275L3 12l5.813 1.912a2 2 0 0 1 1.275 1.275L12 21l1.912-5.813a2 2 0 0 1 1.275-1.275L21 12l-5.813-1.912a2 2 0 0 1-1.275-1.275L12 3Z"/>
              <path d="M5 3v4M3 5h4"/>
            </svg>
          </div>

          <div className="w-full max-w-2xl bg-[#121212] rounded-xl border border-[#262626] shadow-lg p-2 flex flex-col">
            <div className="text-xs text-[#6B7280] px-4 pt-2 pb-1">Tip: Type a command to start a session</div>
            <div className="flex items-center gap-2 px-4 py-3">
              <span className="text-[#3B82F6] font-bold">+</span>
              <span className="text-sm font-mono text-[#9CA3AF]">&lt;/&gt; Code</span>
              <span className="text-sm font-mono text-[#9CA3AF]">Agent-1.0</span>
              <div className="flex-1" />
              <span className="text-xs text-[#6B7280] flex items-center gap-1"><TerminalIcon size={12}/> Local Engine</span>
            </div>
            <div className="border-t border-[#262626] px-4 py-3 flex items-center gap-4 text-xs text-[#9CA3AF]">
              <span className="flex items-center gap-1 hover:text-white cursor-pointer"><FolderOpen size={14}/> Open directory...</span>
            </div>
          </div>

          <div className="w-full max-w-2xl grid grid-cols-3 gap-4">
            <button className="flex flex-col gap-2 p-4 bg-[#121212] rounded-lg border border-[#262626] hover:border-[#3B82F6] transition-colors text-left group">
              <FolderOpen size={18} className="text-[#9CA3AF] group-hover:text-[#3B82F6]"/>
              <span className="text-sm font-medium">Open project</span>
            </button>
            <button className="flex flex-col gap-2 p-4 bg-[#121212] rounded-lg border border-[#262626] hover:border-[#3B82F6] transition-colors text-left group">
              <GitBranch size={18} className="text-[#9CA3AF] group-hover:text-[#3B82F6]"/>
              <span className="text-sm font-medium">Clone repository</span>
            </button>
            <button className="flex flex-col gap-2 p-4 bg-[#121212] rounded-lg border border-[#262626] hover:border-[#3B82F6] transition-colors text-left group">
              <TerminalIcon size={18} className="text-[#9CA3AF] group-hover:text-[#3B82F6]"/>
              <span className="text-sm font-medium">Connect via SSH</span>
            </button>
          </div>

          <div className="w-full max-w-2xl bg-[#121212] rounded-lg border border-[#262626] overflow-hidden">
            <div className="px-4 py-3 text-xs font-semibold text-[#6B7280] uppercase tracking-wider border-b border-[#262626]">Start new session</div>
            <button className="w-full text-left px-4 py-3 text-sm hover:bg-[#262626]/50 transition-colors">Explore my codebase and diagram how it works</button>
            <button className="w-full text-left px-4 py-3 text-sm hover:bg-[#262626]/50 transition-colors border-t border-[#262626]">Review my latest changes and suggest improvements</button>
            <button className="w-full text-left px-4 py-3 text-sm hover:bg-[#262626]/50 transition-colors border-t border-[#262626]">Write tests for my most critical code paths</button>
          </div>

        </div>
      </div>
    );
  }

  return (
    <div className="h-full w-full bg-[#121212] flex flex-col">
      <div className="flex items-end px-2 pt-2 bg-[#000000] border-b border-[#262626] gap-1 shrink-0">
        <button 
          onClick={() => setActiveTab('editor')}
          className={`flex items-center gap-2 px-4 py-2 text-sm rounded-t-lg transition-colors ${activeTab === 'editor' ? 'bg-[#121212] text-blue-400 border-t border-l border-r border-[#262626]' : 'text-[#9CA3AF] hover:bg-[#262626]/50'}`}
        >
          <Code2 size={16} /> Editor
        </button>
        <button 
          onClick={() => setActiveTab('browser')}
          className={`flex items-center gap-2 px-4 py-2 text-sm rounded-t-lg transition-colors ${activeTab === 'browser' ? 'bg-[#121212] text-blue-400 border-t border-l border-r border-[#262626]' : 'text-[#9CA3AF] hover:bg-[#262626]/50'}`}
        >
          <Globe size={16} /> Browser
        </button>
        <button 
          onClick={() => setActiveTab('planner')}
          className={`flex items-center gap-2 px-4 py-2 text-sm rounded-t-lg transition-colors ${activeTab === 'planner' ? 'bg-[#121212] text-blue-400 border-t border-l border-r border-[#262626]' : 'text-[#9CA3AF] hover:bg-[#262626]/50'}`}
        >
          <FileText size={16} /> Planner
        </button>
      </div>

      <div className="flex-1 relative bg-[#121212] overflow-hidden">
        {activeTab === 'editor' && (
          <div className="absolute inset-0 flex flex-col">
            <div className="px-4 py-2 text-xs font-mono text-[#6B7280] border-b border-[#262626] bg-[#121212]/50 flex items-center shrink-0">
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
          <div className="absolute inset-0 flex flex-col bg-white">
            <div className="flex items-center gap-2 p-2 bg-[#f1f3f4] border-b border-gray-300 shrink-0 text-gray-700">
              <button className="p-1 hover:bg-gray-200 rounded"><ArrowLeft size={16} /></button>
              <button className="p-1 hover:bg-gray-200 rounded"><ArrowRight size={16} /></button>
              <button className="p-1 hover:bg-gray-200 rounded"><RotateCw size={16} /></button>
              <button className="p-1 hover:bg-gray-200 rounded mr-2"><Home size={16} /></button>
              <div className="flex-1 bg-white rounded-full px-4 py-1 text-sm border border-gray-300 shadow-inner overflow-hidden text-ellipsis whitespace-nowrap">
                {browserUrl}
              </div>
            </div>
            <div className="flex-1 bg-white flex items-center justify-center">
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
          <div className="absolute inset-0 overflow-y-auto p-8 text-[#F9FAFB] prose prose-invert max-w-none">
            <ReactMarkdown remarkPlugins={[remarkGfm]}>
              {plannerContent}
            </ReactMarkdown>
          </div>
        )}
      </div>
    </div>
  );
}
