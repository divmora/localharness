import { useState, useMemo } from 'react';
import Editor from '@monaco-editor/react';
import { Code2, Globe, FileText } from 'lucide-react';
import { StepUpdate } from '../gen/localharness/v1/localharness_pb';

interface EditorPanelProps {
  steps?: StepUpdate[];
}

export function EditorPanel({ steps = [] }: EditorPanelProps) {
  const [activeTab, setActiveTab] = useState<'editor' | 'browser' | 'planner'>('editor');

  const { filePath, fileContent } = useMemo(() => {
    // Traverse steps backwards to find the most recent file action
    for (let i = steps.length - 1; i >= 0; i--) {
      const action = steps[i].action;
      if (action.case === 'writeToFile') {
        return { 
          filePath: action.value.path, 
          fileContent: action.value.content 
        };
      } else if (action.case === 'replaceFileContent') {
        // Simple fallback, in a real app we'd need to fetch the file to show replacements perfectly,
        // but for now we'll just show the path.
        return { 
          filePath: action.value.path, 
          fileContent: "// File modified via replaceFileContent..." 
        };
      } else if (action.case === 'viewFile') {
        return { 
          filePath: action.value.path, 
          fileContent: action.value.content || "// File viewed..." 
        };
      }
    }
    return { filePath: 'Welcome.md', fileContent: '# Welcome to Divmora\n\nWaiting for agent actions...' };
  }, [steps]);

  return (
    <div className="h-full w-full bg-[#1e1e2e] flex flex-col">
      <div className="flex items-end px-2 pt-2 bg-[#11111b] border-b border-[#313244] gap-1">
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

      <div className="flex-1 relative bg-[#1e1e2e]">
        {activeTab === 'editor' && (
          <div className="absolute inset-0 flex flex-col">
            <div className="px-4 py-2 text-xs font-mono text-[#7f849c] border-b border-[#313244] bg-[#1e1e2e]/50 flex items-center">
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
          <div className="absolute inset-0 flex items-center justify-center text-[#7f849c]">
            Browser integration coming soon...
          </div>
        )}
        {activeTab === 'planner' && (
          <div className="absolute inset-0 flex items-center justify-center text-[#7f849c]">
            Implementation plan view coming soon...
          </div>
        )}
      </div>
    </div>
  );
}
