import Editor from '@monaco-editor/react';

export function EditorPanel() {
  return (
    <div className="h-full w-full bg-[#1e1e2e] flex flex-col">
      <div className="h-10 border-b border-[#313244] flex items-center px-4 text-sm font-mono text-[#a6adc8] bg-[#11111b]">
        src/App.tsx
      </div>
      <div className="flex-1">
        <Editor 
          height="100%" 
          defaultLanguage="typescript" 
          theme="vs-dark"
          defaultValue="// Write your code here"
          options={{ 
            minimap: { enabled: false }, 
            fontSize: 14,
            fontFamily: "'JetBrains Mono', 'Fira Code', monospace",
            padding: { top: 16 }
          }}
        />
      </div>
    </div>
  );
}
