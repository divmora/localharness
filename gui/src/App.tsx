import { useState } from 'react';
import { Panel, Group as PanelGroup, Separator as PanelResizeHandle } from 'react-resizable-panels';
import { Sidebar } from './components/Sidebar';
import { SessionsPanel } from './components/SessionsPanel';
import { ChatPanel } from './components/ChatPanel';
import { EditorPanel } from './components/EditorPanel';
import { TerminalPanel } from './components/TerminalPanel';
import './App.css';
import { useHarness } from './hooks/useHarness';

function App() {
  const [activeSessionId, setActiveSessionId] = useState<string | null>(null);
  const { connected, steps, sendPrompt, submitQuestionResponse } = useHarness(activeSessionId);

  return (
    <div className="flex h-screen w-screen overflow-hidden bg-[#11111b] text-white">
      <Sidebar />
      <PanelGroup orientation="horizontal" className="flex-1">
        
        {/* Left Pane: Sessions/Spaces */}
        <Panel defaultSize={20} minSize={15}>
          <SessionsPanel activeSessionId={activeSessionId} onSelectSession={setActiveSessionId} />
        </Panel>
        
        <PanelResizeHandle className="w-1 bg-[#181825] hover:bg-[#89b4fa]/50 transition-colors" />
        
        {/* Center Pane: Editor/Dashboard + Terminal */}
        <Panel defaultSize={55} minSize={30}>
          <PanelGroup orientation="vertical">
            <Panel defaultSize={70}>
              <EditorPanel steps={steps} />
            </Panel>
            
            <PanelResizeHandle className="h-1 bg-[#181825] hover:bg-[#89b4fa]/50 transition-colors" />
            
            <Panel defaultSize={30} minSize={10}>
              <TerminalPanel steps={steps} />
            </Panel>
          </PanelGroup>
        </Panel>

        <PanelResizeHandle className="w-1 bg-[#181825] hover:bg-[#89b4fa]/50 transition-colors" />

        {/* Right Pane: Chat/Agent */}
        <Panel defaultSize={25} minSize={20}>
          <ChatPanel connected={connected} steps={steps} onSend={sendPrompt} onSubmitQuestionResponse={submitQuestionResponse} />
        </Panel>

      </PanelGroup>
    </div>
  );
}

export default App;
