import { useState, useEffect } from 'react';
import { Panel, Group as PanelGroup, Separator as PanelResizeHandle } from 'react-resizable-panels';
import { UnifiedSidebar } from './components/UnifiedSidebar';
import { CenteredEmptyState } from './components/CenteredEmptyState';
import { ChatPanel } from './components/ChatPanel';
import { WorkspacePanel } from './components/WorkspacePanel';
import { CustomizationsPage } from './components/CustomizationsPage';
import { ConnectSSHModal } from './components/ConnectSSHModal';
import { SessionsManager } from './components/SessionsManager';
import './App.css';
import { useHarness, ConnectionTarget } from './hooks/useHarness';
import { invoke } from '@tauri-apps/api/core';
import { fromBinary } from '@bufbuild/protobuf';
import { SessionListSchema, SessionInfo as ProtoSessionInfo } from './gen/localharness/v1/localharness_pb';
import { WebviewWindow } from '@tauri-apps/api/webviewWindow';

export interface Space {
  id: string;
  name: string;
  target_kind: string;
  target_host: string | null;
}

function App() {
  const [activeSessionId, setActiveSessionId] = useState<string | null>(() => {
    const params = new URLSearchParams(window.location.search);
    return params.get("new_session") === "true" ? crypto.randomUUID() : null;
  });
  
  const [connectionTarget, setConnectionTarget] = useState<ConnectionTarget | null>(() => {
    const params = new URLSearchParams(window.location.search);
    const kind = params.get("kind");
    if (kind === "ssh") {
      return {
        kind: "ssh",
        host: params.get("host") || undefined,
        user: params.get("user") || undefined,
        port: params.get("port") ? parseInt(params.get("port")!, 10) : undefined,
        key_path: params.get("key_path") || undefined,
      };
    }
    return null;
  });

  const [sessions, setSessions] = useState<ProtoSessionInfo[]>([]);
  const [currentView, setCurrentView] = useState<'main' | 'customizations' | 'sessions'>('main');
  const [sshModalOpen, setSshModalOpen] = useState(false);
  const [workspace, setWorkspace] = useState<string | null>(null);
  
  const [spaces, setSpaces] = useState<Space[]>([]);
  const [sessionSpaces, setSessionSpaces] = useState<Record<string, string>>({});
  const [installationId, setInstallationId] = useState<string | null>(null);
  
  const { connected, steps, sendPrompt, submitQuestionResponse, submitPermissionResponse } = useHarness(activeSessionId, connectionTarget, workspace);

  useEffect(() => {
    async function loadSessions() {
      try {
        const iid = await invoke<string>('get_installation_id', { target: connectionTarget });
        setInstallationId(iid);

        const [result, spacesList, sessionMap] = await Promise.all([
          invoke<number[]>('list_sessions', { target: connectionTarget }),
          invoke<Space[]>('get_spaces', { 
            installationId: iid
          }),
          invoke<Record<string, string>>('get_session_spaces')
        ]);
        const sessionList = fromBinary(SessionListSchema, new Uint8Array(result));
        setSessions(sessionList.sessions);
        setSpaces(spacesList);
        setSessionSpaces(sessionMap);
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

  const handleConnectSSH = async (target: ConnectionTarget) => {
    if (connectionTarget === null && !activeSessionId && sessions.length === 0) {
      // Use current window if it's completely empty
      setConnectionTarget(target);
      setActiveSessionId(crypto.randomUUID());
    } else {
      // Spawn new window
      const searchParams = new URLSearchParams();
      searchParams.set("kind", target.kind);
      if (target.host) searchParams.set("host", target.host);
      if (target.user) searchParams.set("user", target.user);
      if (target.port) searchParams.set("port", target.port.toString());
      if (target.key_path) searchParams.set("key_path", target.key_path);
      searchParams.set("new_session", "true");
      
      new WebviewWindow(`ssh-${Date.now()}`, {
        url: `/?${searchParams.toString()}`,
        title: `SSH: ${target.host}`,
        width: 1200,
        height: 800
      });
    }
  };

  const handleCreateSpace = async () => {
    if (!installationId) return;
    const spaceName = window.prompt("Enter new Space name:");
    if (!spaceName || spaceName.trim() === "") return;
    try {
      await invoke('create_space', {
        id: crypto.randomUUID(),
        name: spaceName.trim(),
        installationId: installationId
      });
      // Force reload to get the new space
      if (!activeSessionId) {
        // Just trigger a state update or we can just reload directly
        setActiveSessionId(null);
      }
      // Re-run the loadSessions effect
      const [spacesList] = await Promise.all([
        invoke<Space[]>('get_spaces', { 
          installationId: installationId
        })
      ]);
      setSpaces(spacesList);
    } catch (err) {
      console.error("Failed to create space:", err);
      alert("Failed to create space: " + err);
    }
  };

  const handleMoveSessionToSpace = async (sessionId: string, spaceId: string) => {
    try {
      await invoke('move_session_to_space', {
        sessionId,
        spaceId
      });
      // Re-run the loadSessions effect
      const [sessionMap] = await Promise.all([
        invoke<Record<string, string>>('get_session_spaces')
      ]);
      setSessionSpaces(sessionMap);
    } catch (err) {
      console.error("Failed to move session:", err);
      alert("Failed to move session: " + err);
    }
  };

  return (
    <div className="flex h-screen w-screen overflow-hidden bg-[#000000] text-white">
      <ConnectSSHModal
        isOpen={sshModalOpen}
        onClose={() => setSshModalOpen(false)}
        onConnect={handleConnectSSH}
      />

      <UnifiedSidebar 
        activeSessionId={activeSessionId} 
        onSelectSession={setActiveSessionId} 
        onNewSession={handleNewSession}
        onCreateSpace={handleCreateSpace}
        onMoveSessionToSpace={handleMoveSessionToSpace}
        onOpenCustomizations={() => setCurrentView('customizations')}
        onOpenSessionsManager={() => setCurrentView('sessions')}
        sessions={sessions}
        spaces={spaces}
        sessionSpaces={sessionSpaces}
        mcpServerCount={0}
      />

      {currentView === 'customizations' ? (
        <CustomizationsPage 
          onClose={() => setCurrentView('main')} 
          connectionTarget={connectionTarget}
        />
      ) : currentView === 'sessions' ? (
        <SessionsManager sessions={sessions} />
      ) : !activeSessionId ? (
        <CenteredEmptyState 
          sessions={sessions} 
          onSelectSession={setActiveSessionId} 
          onSubmitPrompt={handleStartPromptSession}
          onOpenSSHModal={() => setSshModalOpen(true)}
          onOpenSessionsManager={() => setCurrentView('sessions')}
          connectionTarget={connectionTarget}
          workspace={workspace}
          onSelectWorkspace={setWorkspace}
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
            <WorkspacePanel 
              steps={steps} 
              onNewSession={handleNewSession} 
              onOpenCustomizations={() => setCurrentView('customizations')}
            />
          </Panel>
        </PanelGroup>
      )}
    </div>
  );
}

export default App;
