import { useState, useEffect } from 'react';
import { CustomizationsPage } from './components/CustomizationsPage';
import { SessionsManager } from './components/SessionsManager';
import { CommandPalette } from './components/CommandPalette';
import { TopBar } from './components/TopBar';
import { OfficeView } from './components/OfficeView';
import { ToastProvider } from './components/Toast';
import { MainPage } from './pages/MainPage';
import './App.css';
import { useHarness } from './hooks/useHarness';
import { invoke } from '@tauri-apps/api/core';
import { fromBinary } from '@bufbuild/protobuf';
import { SessionListSchema, SessionInfo as ProtoSessionInfo } from './gen/localharness/v1/localharness_pb';

export interface Space {
  id: string;
  name: string;
  target_kind: string;
  target_host: string | null;
  office_id: string;
}

export interface Office {
  id: string;
  name: string;
}

function App() {
  const [activeSessionId, setActiveSessionId] = useState<string | null>(() => {
    const params = new URLSearchParams(window.location.search);
    return params.get("new_session") === "true" ? crypto.randomUUID() : null;
  });

  const [sessions, setSessions] = useState<ProtoSessionInfo[]>([]);
  const [currentView, setCurrentView] = useState<'main' | 'customizations' | 'sessions' | 'office'>('main');
  const [workspace, setWorkspace] = useState<string | null>(null);
  
  const [spaces, setSpaces] = useState<Space[]>([]);
  const [sessionSpaces, setSessionSpaces] = useState<Record<string, string>>({});
  const [installationId, setInstallationId] = useState<string | null>(null);
  
  const [offices, setOffices] = useState<Office[]>([]);
  const [activeOfficeId, setActiveOfficeId] = useState<string>(() => {
    const params = new URLSearchParams(window.location.search);
    return params.get("office_id") || 'default';
  });
  
  const { connected, connectionError, steps, trajectoryState, sendPrompt, submitQuestionResponse, submitPermissionResponse, interrupt, resume } = useHarness(activeSessionId, workspace);

  const [showAgentSidebar, setShowAgentSidebar] = useState(true);

  const filteredSessions = sessions.filter(session => {
    const spaceId = sessionSpaces[session.id];
    return spaceId && spaces.some(s => s.id === spaceId);
  });

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
        const iid = await invoke<string>('get_installation_id', { target: null });
        setInstallationId(iid);

        const [result, officesList, spacesList, sessionMap] = await Promise.all([
          invoke<number[]>('list_sessions', { target: null }),
          invoke<Office[]>('get_offices'),
          invoke<Space[]>('get_spaces', { 
            installationId: iid,
            officeId: activeOfficeId
          }),
          invoke<Record<string, string>>('get_session_spaces')
        ]);
        const sessionList = fromBinary(SessionListSchema, new Uint8Array(result));
        setSessions(sessionList.sessions);
        setOffices(officesList);
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
  }, [activeSessionId, activeOfficeId]);

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

  const handleCreateOffice = async () => {
    const name = window.prompt("Enter new Office name:");
    if (name) {
      const id = crypto.randomUUID();
      await invoke('create_office', { id, name: name.trim() });
      // Re-fetch offices
      const officesList = await invoke<Office[]>('get_offices');
      setOffices(officesList);
      setActiveOfficeId(id);
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
        installationId: installationId,
        officeId: activeOfficeId
      });
      // Force reload to get the new space
      if (!activeSessionId) {
        // Just trigger a state update or we can just reload directly
        setActiveSessionId(null);
      }
      // Re-run the loadSessions effect
      const [spacesList] = await Promise.all([
        invoke<Space[]>('get_spaces', { 
          installationId: installationId,
          officeId: activeOfficeId
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
        <TopBar 
          currentView={currentView} 
          onViewChange={setCurrentView} 
          offices={offices}
          activeOfficeId={activeOfficeId}
          onSelectOffice={setActiveOfficeId}
          onCreateOffice={handleCreateOffice}
        />
        <div className="flex flex-1 overflow-hidden relative">
          <CommandPalette />

          {currentView === 'customizations' ? (
            <CustomizationsPage 
              onClose={() => setCurrentView('main')} 
            />
          ) : currentView === 'sessions' ? (
            <SessionsManager sessions={filteredSessions} onSelectSession={handleSelectSession} />
          ) : currentView === 'office' ? (
            <OfficeView 
              sessions={filteredSessions} 
              spaces={spaces}
              sessionSpaces={sessionSpaces}
              onSelectSession={handleSelectSession} 
            />
          ) : (
            <MainPage
              activeSessionId={activeSessionId}
              sessions={filteredSessions}
              spaces={spaces}
              sessionSpaces={sessionSpaces}
              showAgentSidebar={showAgentSidebar}
              connected={connected}
              connectionError={connectionError}
              steps={steps}
              workspace={workspace}
              onSelectSession={handleSelectSession}
              onNewSession={handleNewSession}
              onCreateSpace={handleCreateSpace}
              onMoveSessionToSpace={handleMoveSessionToSpace}
              onOpenCustomizations={() => setCurrentView('customizations')}
              onOpenSessionsManager={() => setCurrentView('sessions')}
              onSubmitPrompt={handleStartPromptSession}
              onSubmitQuestionResponse={submitQuestionResponse}
              onSubmitPermissionResponse={submitPermissionResponse}
              onSelectWorkspace={setWorkspace}
              trajectoryState={trajectoryState}
              onInterrupt={interrupt}
              onResume={resume}
            />
          )}
        </div>
      </div>
    </ToastProvider>
  );
}

export default App;
