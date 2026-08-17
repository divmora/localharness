import { useState, useEffect } from 'react';
import { Panel, Group as PanelGroup, Separator as PanelResizeHandle } from 'react-resizable-panels';
import { AgentSidebar } from './components/AgentSidebar';
import { CenteredEmptyState } from './components/CenteredEmptyState';
import { ChatPanel } from './components/ChatPanel';
import { WorkspacePanel } from './components/WorkspacePanel';
import { CustomizationsPage } from './components/CustomizationsPage';
import { ConnectSSHModal } from './components/ConnectSSHModal';
import { SessionsManager } from './components/SessionsManager';
import { CommandPalette } from './components/CommandPalette';
import { TopBar } from './components/TopBar';
import { TerminalPanel } from './components/TerminalPanel';
import { OfficeView } from './components/OfficeView';
import { ToastProvider } from './components/Toast';
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
  const [currentView, setCurrentView] = useState<'main' | 'customizations' | 'sessions' | 'office'>('main');
  const [sshModalOpen, setSshModalOpen] = useState(false);
  const [workspace, setWorkspace] = useState<string | null>(null);
  
  const [spaces, setSpaces] = useState<Space[]>([]);
  const [sessionSpaces, setSessionSpaces] = useState<Record<string, string>>({});
  const [installationId, setInstallationId] = useState<string | null>(null);
  
  const { connected, connectionError, steps, sendPrompt, submitQuestionResponse, submitPermissionResponse } = useHarness(activeSessionId, connectionTarget, workspace);

  const [showAgentSidebar, setShowAgentSidebar] = useState(true);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Cmd+B on Mac, Ctrl+B on Windows/Linux
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'b') {
        e.preventDefault();
        setShowAgentSidebar(prev => !prev);
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, []);

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

  const handleSelectSession = (id: string) => {
    setActiveSessionId(id);
    setCurrentView('main');
  };

  const handleStartPromptSession = (prompt: string) => {
    const newId = crypto.randomUUID();
    setActiveSessionId(newId);
    sendPrompt(prompt);
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
        title: `SSH: ${target.host || 'Local'}`,
        width: 1200,
        height: 800
      }).once('tauri://error', function (e) {
        console.error("Window creation error:", e);
        alert("Failed to open window: " + JSON.stringify(e));
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
    <ToastProvider>
      <div className="flex flex-col h-screen w-screen overflow-hidden bg-bg-primary text-text-primary transition-colors">
        <TopBar currentView={currentView} onViewChange={setCurrentView as any} />
        <div className="flex flex-1 overflow-hidden relative">
          <CommandPalette />
          <ConnectSSHModal
            isOpen={sshModalOpen}
            onClose={() => setSshModalOpen(false)}
            onConnect={handleConnectSSH}
          />

          <PanelGroup orientation="horizontal" className="flex-1">
          {/* Left Pane: Agent Sidebar */}
          {showAgentSidebar && (
            <>
              <Panel defaultSize={20} minSize={15} className="flex flex-col" collapsible>
                <AgentSidebar 
                  activeSessionId={activeSessionId} 
                  onSelectSession={handleSelectSession} 
                  onNewSession={handleNewSession}
                  onCreateSpace={handleCreateSpace}
                  onMoveSessionToSpace={handleMoveSessionToSpace}
                  onOpenCustomizations={() => setCurrentView('customizations')}
                  onOpenSessionsManager={() => setCurrentView('sessions')}
                  onOpenOffice={() => setCurrentView('office')}
                  sessions={sessions}
                  spaces={spaces}
                  sessionSpaces={sessionSpaces}
                  mcpServerCount={0}
                />
              </Panel>
              <PanelResizeHandle className="w-[1px] bg-border-primary transition-colors hover:opacity-80" style={{ backgroundColor: 'var(--border-primary)' }} />
            </>
          )}

          {/* Main Workspace Area (Chat + Editor + Terminal) */}
          <Panel className="flex flex-col bg-bg-primary">
            <PanelGroup orientation="vertical">
              {/* Top Section: Chat + Editor */}
              <Panel defaultSize={70} minSize={20} className="flex flex-col">
                <PanelGroup orientation="horizontal">
                  {/* Chat / Main Content */}
                  <Panel defaultSize={50} minSize={30} className="flex flex-col">
                    {currentView === 'customizations' ? (
                      <CustomizationsPage 
                        onClose={() => setCurrentView('main')} 
                        connectionTarget={connectionTarget}
                      />
                    ) : currentView === 'sessions' ? (
                      <SessionsManager sessions={sessions} onSelectSession={handleSelectSession} />
                    ) : currentView === 'office' ? (
                      <OfficeView sessions={sessions} onSelectSession={handleSelectSession} />
                    ) : !activeSessionId ? (
                      <CenteredEmptyState 
                        sessions={sessions} 
                        onSelectSession={handleSelectSession} 
                        onSubmitPrompt={handleStartPromptSession}
                        onOpenSSHModal={() => setSshModalOpen(true)}
                        onOpenSessionsManager={() => setCurrentView('sessions')}
                        connectionTarget={connectionTarget}
                        workspace={workspace}
                        onSelectWorkspace={setWorkspace}
                      />
                    ) : (
                      <ChatPanel 
                        connected={connected} 
                        connectionError={connectionError}
                        steps={steps} 
                        onSend={sendPrompt} 
                        onSubmitQuestionResponse={submitQuestionResponse} 
                        onSubmitPermissionResponse={submitPermissionResponse}
                      />
                    )}
                  </Panel>

                  <PanelResizeHandle className="w-[1px] bg-border-primary transition-colors hover:opacity-80" style={{ backgroundColor: 'var(--border-primary)' }} />

                  {/* Editor */}
                  <Panel defaultSize={50} minSize={20} className="flex flex-col">
                    <WorkspacePanel 
                      steps={steps} 
                      onNewSession={handleNewSession} 
                      onOpenCustomizations={() => setCurrentView('customizations')}
                    />
                  </Panel>
                </PanelGroup>
              </Panel>
              
              <PanelResizeHandle className="h-[1px] bg-border-primary transition-colors hover:opacity-80" />
              
              {/* Bottom Section: Terminal */}
              <Panel defaultSize={30} minSize={10} className="relative bg-bg-secondary">
                 <div className="absolute top-0 left-0 right-0 bg-bg-primary px-3 py-1.5 border-b border-border-primary z-10 flex items-center gap-2">
                   <span className="text-[10px] font-semibold text-text-tertiary uppercase tracking-wider">Terminal Output</span>
                 </div>
                 <div className="pt-8 h-full">
                   <TerminalPanel steps={steps} />
                 </div>
              </Panel>
            </PanelGroup>
          </Panel>
        </PanelGroup>
      </div>
    </div>
    </ToastProvider>
  );
}

export default App;
