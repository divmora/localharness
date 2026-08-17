import { useState, useEffect, useCallback } from 'react';
import { CustomizationsPage } from './components/CustomizationsPage';
import { SessionsManager } from './components/SessionsManager';
import { CommandPalette } from './components/CommandPalette';
import { TopBar } from './components/TopBar';
import { OfficeView } from './components/OfficeView';
import { CreateOfficeModal } from './components/CreateOfficeModal';
import { PromptModal } from './components/PromptModal';
import { ConfirmModal } from './components/ConfirmModal';
import { MainPage } from './pages/MainPage';
import './App.css';
import { useHarness } from './hooks/useHarness';
import { useToast } from './components/Toast';
import { invoke } from '@tauri-apps/api/core';
import { fromBinary } from '@bufbuild/protobuf';
import { SessionListSchema, SessionInfo as ProtoSessionInfo } from './gen/localharness/v1/localharness_pb';
import { usePersistentState } from './hooks/usePersistentState';

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
  const [activeSessionId, setActiveSessionId] = useState<string | null>(null);
  const [initialBudget, setInitialBudget] = useState<number>(0);
  const [currentView, setCurrentView] = usePersistentState<'main' | 'customizations' | 'sessions' | 'office'>('ui.currentView', 'main');
  const [workspace, setWorkspace] = useState<string | null>(null);
  
  const [sessions, setSessions] = useState<ProtoSessionInfo[]>([]);
  const [spaces, setSpaces] = useState<Space[]>([]);
  const [sessionSpaces, setSessionSpaces] = useState<Record<string, string>>({});
  const [officeManagers, setOfficeManagers] = useState<Record<string, string>>({});
  const [installationId, setInstallationId] = useState<string | null>(null);
  
  const [offices, setOffices] = useState<Office[]>([]);
  const [activeOfficeId, setActiveOfficeId] = usePersistentState<string>('ui.activeOfficeId', 'default');
  
  // Modal states
  const [officePromptState, setOfficePromptState] = useState<{ isOpen: boolean }>({ isOpen: false });
  const [spacePromptState, setSpacePromptState] = useState<{ isOpen: boolean }>({ isOpen: false });
  const [deleteConfirmState, setDeleteConfirmState] = useState<{ isOpen: boolean, sessionId?: string }>({ isOpen: false });

  const { showToast } = useToast();
  
  const { connected, connectionError, steps, trajectoryState, sendPrompt, submitQuestionResponse, submitPermissionResponse, interrupt, resume } = useHarness(activeSessionId, workspace, initialBudget, false);

  const [showAgentSidebar, setShowAgentSidebar] = usePersistentState('ui.showAgentSidebar', true);
  const [showTerminal, setShowTerminal] = usePersistentState('ui.showTerminal', true);

  // Manager session IDs
  const managerSessionIds = new Set(Object.values(officeManagers));

  const filteredSessions = sessions.filter(session => {
    // Hide office managers from the Chat view
    if (currentView !== 'office' && managerSessionIds.has(session.id)) {
      return false;
    }

    const spaceId = sessionSpaces[session.id];
    if (!spaceId) {
      return true; // Show unassigned sessions
    }
    // If assigned to a space, only show if that space belongs to the currently active office
    return spaces.some(s => s.id === spaceId);
  });

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Cmd+B on Mac, Ctrl+B on Windows/Linux
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'b') {
        e.preventDefault();
        setShowAgentSidebar(!showAgentSidebar);
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, []);

  const loadSessions = useCallback(async () => {
    try {
      const iid = await invoke<string>('get_installation_id', { target: null });
      setInstallationId(iid);

      const [result, officesList, spacesList, sessionMap, managerMap] = await Promise.all([
        invoke<number[]>('list_sessions', { target: null }),
        invoke<Office[]>('get_offices'),
        invoke<Space[]>('get_spaces', { 
          installationId: iid,
          officeId: activeOfficeId
        }),
        invoke<Record<string, string>>('get_session_spaces'),
        invoke<Record<string, string>>('get_all_office_managers')
      ]);
      const sessionList = fromBinary(SessionListSchema, new Uint8Array(result));
      setSessions(sessionList.sessions);
      setOffices(officesList);
      setSpaces(spacesList);
      setSessionSpaces(sessionMap);
      setOfficeManagers(managerMap);
    } catch (err) {
      console.error("Failed to list sessions:", err);
    }
  }, [activeOfficeId]);

  useEffect(() => {
    loadSessions();
    
    const interval = setInterval(() => {
      if (!activeSessionId) {
        loadSessions();
      }
    }, 5000);
    return () => clearInterval(interval);
  }, [activeSessionId, loadSessions]);

  const handleNewSession = () => {
    setActiveSessionId(null);
  };

  const handleSelectSession = (id: string) => {
    setActiveSessionId(id);
    setCurrentView('main');
  };

  const handleStartPromptSession = async (prompt: string, allocatedBudget?: number) => {
    const newId = crypto.randomUUID();
    setInitialBudget(allocatedBudget || 0);

    // Deduct from Office Wallet if budget is allocated
    if (allocatedBudget && allocatedBudget > 0) {
      try {
        await invoke('add_wallet_balance', { officeId: activeOfficeId, amount: -allocatedBudget });
      } catch (e) {
        console.error("Failed to deduct budget from office wallet:", e);
      }
    }

    setSessions(prev => [{
      id: newId,
      name: 'New Session',
      status: 0,
      createdAt: BigInt(Date.now()),
      updatedAt: BigInt(Date.now()),
      workspace: workspace || '',
      budgetAllocated: allocatedBudget || 0,
      budgetSpent: 0
    } as any, ...prev]);

    setActiveSessionId(newId);
    sendPrompt(prompt);
  };

  const handleCreateOffice = () => {
    setOfficePromptState({ isOpen: true });
  };

  const confirmCreateOffice = async (name: string, country: string) => {
    if (name && name.trim() !== '') {
      const id = crypto.randomUUID();
      try {
        await invoke('create_office', { id, name: name.trim(), country });
        const officesList = await invoke<Office[]>('get_offices');
        setOffices(officesList);
        setActiveOfficeId(id);
      } catch (err) {
        console.error("Failed to create office:", err);
        showToast({ title: 'Error', message: 'Failed to create office', type: 'error' });
      }
    }
    setOfficePromptState({ isOpen: false });
  };

  const handleDeleteSession = (sessionId: string) => {
    setDeleteConfirmState({ isOpen: true, sessionId });
  };

  const confirmDeleteSession = async () => {
    const sessionId = deleteConfirmState.sessionId;
    setDeleteConfirmState({ isOpen: false });
    if (!sessionId) return;
    
    try {
      await invoke('delete_session', { sessionId, target: null });
      if (activeSessionId === sessionId) {
        setActiveSessionId(null);
      }
      loadSessions();
      showToast({ title: 'Success', message: 'Session deleted', type: 'success' });
    } catch (e) {
      console.error("Failed to delete session:", e);
      showToast({ title: 'Error', message: `Failed to delete session: ${e}`, type: 'error' });
    }
  };

  const handleCreateSpace = () => {
    if (!installationId) return;
    setSpacePromptState({ isOpen: true });
  };

  const confirmCreateSpace = async (spaceName: string) => {
    setSpacePromptState({ isOpen: false });
    if (!installationId) return;
    if (!spaceName || spaceName.trim() === "") return;
    try {
      await invoke('create_space', {
        id: crypto.randomUUID(),
        name: spaceName.trim(),
        installationId: installationId,
        officeId: activeOfficeId
      });
      if (!activeSessionId) {
        setActiveSessionId(null);
      }
      const [spacesList] = await Promise.all([
        invoke<Space[]>('get_spaces', { 
          installationId: installationId,
          officeId: activeOfficeId
        })
      ]);
      setSpaces(spacesList);
    } catch (err) {
      console.error("Failed to create space:", err);
      showToast({ title: 'Error', message: `Failed to create space: ${err}`, type: 'error' });
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
      showToast({ title: 'Success', message: 'Session moved to space', type: 'success' });
    } catch (err) {
      console.error("Failed to move session:", err);
      showToast({ title: 'Error', message: `Failed to move session: ${err}`, type: 'error' });
    }
  };

  const handleManagerCreated = useCallback(async (sessionId: string, officeId: string) => {
    try {
      await invoke('set_office_manager', { officeId: officeId, managerSessionId: sessionId });
      setOfficeManagers(prev => ({ ...prev, [officeId]: sessionId }));
    } catch (err) {
      console.error("Failed to set office manager", err);
      showToast({ title: 'Error', message: `Failed to set manager: ${err}`, type: 'error' });
    }
  }, [showToast]);

  return (
    <>
      <div className="flex flex-col h-screen w-screen overflow-hidden bg-bg-primary text-text-primary transition-colors">
      <TopBar 
          currentView={currentView} 
          onViewChange={setCurrentView} 
          offices={offices}
          activeOfficeId={activeOfficeId}
          onSelectOffice={setActiveOfficeId}
          onCreateOffice={handleCreateOffice}
          isChatMode={activeSessionId !== null && currentView === 'main'}
          showTerminal={showTerminal}
          onToggleTerminal={() => setShowTerminal(!showTerminal)}
          showSidebar={showAgentSidebar}
          onToggleSidebar={() => setShowAgentSidebar(!showAgentSidebar)}
        />
        <div className="flex flex-1 overflow-hidden relative">
          <CommandPalette />

          {currentView === 'customizations' ? (
            <CustomizationsPage 
              onClose={() => setCurrentView('main')} 
            />
          ) : currentView === 'sessions' ? (
            <SessionsManager sessions={filteredSessions} onSelectSession={handleSelectSession} onDeleteSession={handleDeleteSession} />
          ) : currentView === 'office' ? (
            <OfficeView 
              sessions={filteredSessions} 
              onSelectSession={handleSelectSession}
              spaces={spaces}
              sessionSpaces={sessionSpaces}
              activeOfficeId={activeOfficeId}
              managerSessionId={officeManagers[activeOfficeId]}
              onManagerCreated={(sessionId) => handleManagerCreated(sessionId, activeOfficeId)}
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
              onDeleteSession={handleDeleteSession}
              trajectoryState={trajectoryState}
              onInterrupt={interrupt}
              onResume={resume}
              showTerminal={showTerminal}
            />
          )}
        </div>
      </div>

      {officePromptState.isOpen && (
        <CreateOfficeModal
          onConfirm={confirmCreateOffice}
          onCancel={() => setOfficePromptState({ isOpen: false })}
        />
      )}

      {spacePromptState.isOpen && (
        <PromptModal
          title="Create New Space"
          placeholder="Enter Space name..."
          confirmText="Create Space"
          onConfirm={confirmCreateSpace}
          onCancel={() => setSpacePromptState({ isOpen: false })}
        />
      )}

      {deleteConfirmState.isOpen && (
        <ConfirmModal
          title="Delete Session"
          message="Are you sure you want to delete this session? This action cannot be undone."
          confirmText="Delete"
          destructive={true}
          onConfirm={confirmDeleteSession}
          onCancel={() => setDeleteConfirmState({ isOpen: false, sessionId: undefined })}
        />
      )}
    </>
  );
}

export default App;
