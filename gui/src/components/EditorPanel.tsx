import { useState, useMemo } from 'react';
import Editor from '@monaco-editor/react';
import { Code2, Globe, FileText, ArrowLeft, ArrowRight, RotateCw, Home } from 'lucide-react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { StepUpdate } from '../gen/localharness/v1/localharness_pb';

interface EditorPanelProps {
  steps?: StepUpdate[];
}

export function EditorPanel({ steps = [] }: EditorPanelProps) {
  const [activeTab, setActiveTab] = useState<'editor' | 'browser' | 'planner'>('editor');

  const { filePath, fileContent, browserUrl, plannerContent } = useMemo(() => {
    let recentFile = 'Welcome.md';
    let recentFileContent = '# Welcome to Divmora\n\nWaiting for agent actions...';
    let recentBrowserUrl = 'https://google.com';
    let recentPlannerContent = '# No plans yet\n\nWhen the agent creates a task list or implementation plan, it will appear here.';

    // Traverse steps backwards to find the most recent actions for each tab type
    for (let i = steps.length - 1; i >= 0; i--) {
      const action = steps[i].action;
      if (!action.case) continue;

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
        // If it's a known plan file or has artifact metadata, we put it in the planner.
        if (path.endsWith('task.md') || path.endsWith('implementation_plan.md') || path.endsWith('walkthrough.md')) {
           // Only grab the first one we see (most recent)
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
      filePath: recentFile, 
      fileContent: recentFileContent,
      browserUrl: recentBrowserUrl,
      plannerContent: recentPlannerContent
    };
  }, [steps]);

  return (
    <div className="h-full w-full bg-[#1e1e2e] flex flex-col">
      <div className="flex items-end px-2 pt-2 bg-[#11111b] border-b border-[#313244] gap-1 shrink-0">
        <button 
          onClick={() => setActiveTab('editor')}
          className={`flex items-center gap-2 px-4 py-2 text-sm rounded-t-lg transition-colors ${activeTab === 'editor' ? 'bg-[#1e1e2e] text-blue-400 border-t border-l border-r border-[#313244]' : 'text-[#a6adc8] hover:bg-[#313244]/50'}`}
        >
          <Code2 size={16} /> Editor
        </button>
        <button 
          onClick={() => setActiveTab('browser')}
          className={`flex items-center gap-2 px-4 py-2 text-sm rounded-t-lg transition-colors ${activeTab === 'browser' ? 'bg-[#1e1e2e] text-blue-400 border-t border-l border-r border-[#313244]' : 'text-[#a6adc8] hover:bg-[#313244]/50'}`}
        >
          <Globe size={16} /> Browser
        </button>
        <button 
          onClick={() => setActiveTab('planner')}
          className={`flex items-center gap-2 px-4 py-2 text-sm rounded-t-lg transition-colors ${activeTab === 'planner' ? 'bg-[#1e1e2e] text-blue-400 border-t border-l border-r border-[#313244]' : 'text-[#a6adc8] hover:bg-[#313244]/50'}`}
        >
          <FileText size={16} /> Planner
        </button>
      </div>

      <div className="flex-1 relative bg-[#1e1e2e] overflow-hidden">
        {activeTab === 'editor' && (
          <div className="absolute inset-0 flex flex-col">
            <div className="px-4 py-2 text-xs font-mono text-[#7f849c] border-b border-[#313244] bg-[#1e1e2e]/50 flex items-center shrink-0">
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
              {/* Due to x-frame-options, many sites won't load in an iframe. 
                  But this gives the visual feedback that the agent is browsing. */}
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
          <div className="absolute inset-0 overflow-y-auto p-8 text-[#cdd6f4] prose prose-invert max-w-none">
            <ReactMarkdown remarkPlugins={[remarkGfm]}>
              {plannerContent}
            </ReactMarkdown>
          </div>
        )}
      </div>
    </div>
  );
}
