import { useState, useEffect } from 'react';
import { Panel, Group as PanelGroup, Separator as PanelResizeHandle } from 'react-resizable-panels';
import { UnifiedSidebar } from './components/UnifiedSidebar';
import { CenteredEmptyState } from './components/CenteredEmptyState';
import { ChatPanel } from './components/ChatPanel';
import { WorkspacePanel } from './components/WorkspacePanel';
import { CustomizationsModal } from './components/CustomizationsModal';
import { ConnectSSHModal } from './components/ConnectSSHModal';
import './App.css';
import { useHarness, ConnectionTarget } from './hooks/useHarness';
import { invoke } from '@tauri-apps/api/core';
import { fromBinary } from '@bufbuild/protobuf';
import { SessionListSchema, SessionInfo as ProtoSessionInfo } from './gen/localharness/v1/localharness_pb';

function App() {
  const [activeSessionId, setActiveSessionId] = useState<string | null>(null);
  const [connectionTarget, setConnectionTarget] = useState<ConnectionTarget | null>(null);
  const [sessions, setSessions] = useState<ProtoSessionInfo[]>([]);
  const [customizationsOpen, setCustomizationsOpen] = useState(false);
  const [sshModalOpen, setSshModalOpen] = useState(false);
  
  const { connected, steps, sendPrompt, submitQuestionResponse, submitPermissionResponse } = useHarness(activeSessionId, connectionTarget);

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
    
    const interval = setInterval(() => {
      if (!activeSessionId) {
        loadSessions();
      }
    }, 5000);
    return () => clearInterval(interval);
  }, [activeSessionId]);

  const handleNewSession = () => {
    setActiveSessionId(null);
  };

  const handleStartPromptSession = (prompt: string) => {
    const newId = crypto.randomUUID();
    setActiveSessionId(newId);
    // Give the WebSocket a tiny moment to connect before firing off the prompt
    setTimeout(() => {
      sendPrompt(prompt);
    }, 500);
  };

  const handleConnectSSH = (target: ConnectionTarget) => {
    setConnectionTarget(target);
    const newId = crypto.randomUUID();
    setActiveSessionId(newId);
  };

  return (
    <div className="flex h-screen w-screen overflow-hidden bg-[#000000] text-white">
      <CustomizationsModal 
        isOpen={customizationsOpen} 
        onClose={() => setCustomizationsOpen(false)} 
      />
      <ConnectSSHModal
        isOpen={sshModalOpen}
        onClose={() => setSshModalOpen(false)}
        onConnect={handleConnectSSH}
      />

      <UnifiedSidebar 
        activeSessionId={activeSessionId}
        onSelectSession={setActiveSessionId}
        onNewSession={handleNewSession}
        onOpenCustomizations={() => setCustomizationsOpen(true)}
        sessions={sessions}
        mcpServerCount={0}
      />

      {!activeSessionId ? (
        <CenteredEmptyState 
          onSelectSession={setActiveSessionId}
          sessions={sessions}
          onSubmitPrompt={handleStartPromptSession}
          onOpenSSHModal={() => setSshModalOpen(true)}
        />
      ) : (
        <PanelGroup orientation="horizontal" className="flex-1 border-l border-[#0A0A0A]">
          {/* Center Pane: ChatPanel */}
          <Panel defaultSize={55} minSize={40}>
            <ChatPanel 
              connected={connected} 
              steps={steps} 
              onSend={sendPrompt} 
              onSubmitQuestionResponse={submitQuestionResponse} 
              onSubmitPermissionResponse={submitPermissionResponse}
            />
          </Panel>

          <PanelResizeHandle className="w-1 bg-[#0A0A0A] hover:bg-[#3B82F6]/50 transition-colors" />

          {/* Right Pane: Workspace */}
          <Panel defaultSize={45} minSize={20}>
            <WorkspacePanel steps={steps} onNewSession={handleNewSession} />
          </Panel>
        </PanelGroup>
      )}
    </div>
  );
}

export default App;
