import { useState, useEffect } from 'react';
import { Panel, Group as PanelGroup, Separator as PanelResizeHandle } from 'react-resizable-panels';
import { Sidebar } from './components/Sidebar';
import { SessionsPanel } from './components/SessionsPanel';
import { SessionBoard } from './components/SessionBoard';
import { ChatPanel } from './components/ChatPanel';
import { WorkspacePanel } from './components/WorkspacePanel';
import './App.css';
import { useHarness } from './hooks/useHarness';
import { invoke } from '@tauri-apps/api/core';
import { fromBinary } from '@bufbuild/protobuf';
import { SessionListSchema, SessionInfo as ProtoSessionInfo } from './gen/localharness/v1/localharness_pb';

function App() {
  const [activeSessionId, setActiveSessionId] = useState<string | null>(null);
  const [sessions, setSessions] = useState<ProtoSessionInfo[]>([]);
  const { connected, steps, sendPrompt, submitQuestionResponse, submitPermissionResponse } = useHarness(activeSessionId);

  useEffect(() => {
    async function loadSessions() {
      try {
        const result = await invoke<number[]>('list_sessions');
        const sessionList = fromBinary(SessionListSchema, new Uint8Array(result));
        setSessions(sessionList.sessions);
      } catch (err) {
        console.error("Failed to list sessions:", err);
      }
    }
    loadSessions();
    
    // Poll every 5 seconds when no session is active to keep board updated
    const interval = setInterval(() => {
      if (!activeSessionId) {
        loadSessions();
      }
    }, 5000);
    return () => clearInterval(interval);
  }, [activeSessionId]);

  const handleNewSession = () => {
    setActiveSessionId(crypto.randomUUID());
  };

  return (
    <div className="flex h-screen w-screen overflow-hidden bg-[#000000] text-white">
      <Sidebar />
      <PanelGroup orientation="horizontal" className="flex-1">
        
        {/* Left Pane: Sessions/Spaces */}
        <Panel defaultSize={20} minSize={15}>
          <SessionsPanel 
            activeSessionId={activeSessionId} 
            onSelectSession={setActiveSessionId} 
            onNewSession={handleNewSession}
            sessions={sessions}
          />
        </Panel>
        
        <PanelResizeHandle className="w-1 bg-[#0A0A0A] hover:bg-[#3B82F6]/50 transition-colors" />
        
        {/* Center Pane: SessionBoard OR ChatPanel */}
        <Panel defaultSize={55} minSize={40}>
          {activeSessionId ? (
            <ChatPanel 
              connected={connected} 
              steps={steps} 
              onSend={sendPrompt} 
              onSubmitQuestionResponse={submitQuestionResponse} 
              onSubmitPermissionResponse={submitPermissionResponse}
            />
          ) : (
            <SessionBoard 
              sessions={sessions} 
              onSelectSession={setActiveSessionId} 
              onNewSession={handleNewSession}
            />
          )}
        </Panel>

        <PanelResizeHandle className="w-1 bg-[#0A0A0A] hover:bg-[#3B82F6]/50 transition-colors" />

        {/* Right Pane: Workspace */}
        <Panel defaultSize={25} minSize={20}>
          <WorkspacePanel steps={steps} onNewSession={handleNewSession} />
        </Panel>

      </PanelGroup>
    </div>
  );
}

export default App;
