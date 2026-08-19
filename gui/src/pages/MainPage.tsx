import { Panel, Group as PanelGroup, Separator as PanelResizeHandle } from 'react-resizable-panels';
import { AgentSidebar } from '../components/AgentSidebar';
import { ChatPanel } from '../components/ChatPanel';
import { WorkspacePanel } from '../components/WorkspacePanel';
import { TerminalPanel } from '../components/TerminalPanel';
import { SessionInfo as ProtoSessionInfo, StepUpdate, TrajectoryState_TrajState } from '../gen/localharness/v1/localharness_pb';
import { Space } from '../App';
import { CenteredEmptyState } from '../components/CenteredEmptyState';
import { usePixelPanelSizes } from '../hooks/usePixelSize';

interface MainPageProps {
  activeSessionId: string | null;
  sessions: ProtoSessionInfo[];
  spaces: Space[];
  sessionSpaces: Record<string, string>;
  showAgentSidebar: boolean;
  connected: boolean;
  connectionError: string | null;
  steps: StepUpdate[];
  workspace: string | null;

  onSelectSession: (id: string) => void;
  onNewSession: () => void;
  onCreateSpace: () => void;
  onMoveSessionToSpace: (sessionId: string, spaceId: string) => void;
  onOpenCustomizations: () => void;
  onOpenSessionsManager: () => void;
  onSubmitPrompt: (prompt: string, allocatedBudget?: number) => void;
  onSendPrompt: (prompt: string) => void;
  onSubmitQuestionResponse: (requestId: string, answers: any[], skipped: boolean) => void;
  onSubmitPermissionResponse: (requestId: string, approved: boolean, reason?: string) => void;
  onSelectWorkspace: (path: string) => void;
  onDeleteSession?: (sessionId: string) => void;
  onArchiveSession?: (sessionId: string) => void;
  trajectoryState: TrajectoryState_TrajState;
  onInterrupt: () => void;
  onResume: (msg?: string) => void;
  showTerminal?: boolean;
}

export function MainPage({
  activeSessionId,
  sessions,
  spaces,
  sessionSpaces,
  showAgentSidebar,
  connected,
  connectionError,
  steps,
  workspace,
  onSelectSession,
  onNewSession,
  onCreateSpace,
  onMoveSessionToSpace,
  onOpenCustomizations,
  onOpenSessionsManager,
  onSubmitPrompt,
  onSendPrompt,
  onSubmitQuestionResponse,
  onSubmitPermissionResponse,
  onSelectWorkspace,
  onDeleteSession,
  onArchiveSession,
  trajectoryState,
  onInterrupt,
  onResume,
  showTerminal = true
}: MainPageProps) {
  const sidebarSizes = usePixelPanelSizes(250, 400, 20);

  return (
    // @ts-ignore
    <PanelGroup id="harness-main-layout" orientation="horizontal" className="flex-1 overflow-hidden">
      {/* Left Pane: Agent Sidebar */}
      {showAgentSidebar && (
        <>
          <Panel defaultSize={20} minSize={sidebarSizes.minSize} maxSize={sidebarSizes.maxSize} className="flex flex-col">
            <AgentSidebar
              activeSessionId={activeSessionId}
              onSelectSession={onSelectSession}
              onNewSession={onNewSession}
              onCreateSpace={onCreateSpace}
              onMoveSessionToSpace={onMoveSessionToSpace}
              onOpenCustomizations={onOpenCustomizations}
              onOpenSessionsManager={onOpenSessionsManager}
              onDeleteSession={onDeleteSession}
              onArchiveSession={onArchiveSession}
              sessions={sessions}
              spaces={spaces}
              sessionSpaces={sessionSpaces}
              mcpServerCount={0}
            />
          </Panel>
          <PanelResizeHandle className="w-1.5 flex items-center justify-center cursor-col-resize hover:bg-blue-500/10 transition-colors group z-10">
            <div className="w-[1px] h-full bg-border-primary group-hover:bg-blue-500/50 transition-colors" />
          </PanelResizeHandle>
        </>
      )}

      {/* Main Workspace Area (Chat + Editor + Terminal) */}
      <Panel className="flex flex-col bg-bg-primary">
        {/* @ts-ignore */}
        <PanelGroup id="harness-vertical-layout" orientation="vertical">
          {/* Top Section: Chat + Editor */}
          <Panel defaultSize={70} minSize={20} className="flex flex-col">
            {/* @ts-ignore */}
            <PanelGroup id="harness-chat-editor-layout" orientation="horizontal">
              {/* Chat / Main Content */}
              <Panel defaultSize={50} minSize={30} className="flex flex-col">
                {!activeSessionId ? (
                  <CenteredEmptyState
                    sessions={sessions}
                    onSelectSession={onSelectSession}
                    onSubmitPrompt={onSubmitPrompt}
                    onOpenSessionsManager={onOpenSessionsManager}
                    workspace={workspace}
                    onSelectWorkspace={onSelectWorkspace}
                  />
                ) : (
                  <ChatPanel
                    activeSessionId={activeSessionId}
                    connected={connected}
                    connectionError={connectionError}
                    steps={steps}
                    trajectoryState={trajectoryState}
                    onInterrupt={onInterrupt}
                    onResume={onResume}
                    onSend={onSendPrompt}
                    onSubmitQuestionResponse={onSubmitQuestionResponse}
                    onSubmitPermissionResponse={onSubmitPermissionResponse}
                  />
                )}
              </Panel>

              {activeSessionId && (
                <>
                  <PanelResizeHandle className="w-1.5 flex items-center justify-center cursor-col-resize hover:bg-blue-500/10 transition-colors group z-10">
                    <div className="w-[1px] h-full bg-border-primary group-hover:bg-blue-500/50 transition-colors" />
                  </PanelResizeHandle>

                  {/* Editor */}
                  <Panel defaultSize={50} minSize={20} className="flex flex-col">
                    <WorkspacePanel
                      steps={steps}
                      onNewSession={onNewSession}
                      onOpenCustomizations={onOpenCustomizations}
                    />
                  </Panel>
                </>
              )}
            </PanelGroup>
          </Panel>

          {activeSessionId && showTerminal && (
            <>
              <PanelResizeHandle className="h-1.5 flex flex-col items-center justify-center cursor-row-resize hover:bg-blue-500/10 transition-colors group z-10">
                <div className="h-[1px] w-full bg-border-primary group-hover:bg-blue-500/50 transition-colors" />
              </PanelResizeHandle>

              {/* Bottom Section: Terminal */}
              <Panel
                defaultSize={30}
                minSize={10}
                className="relative bg-bg-secondary"
              >
                <TerminalPanel steps={steps} activeSessionId={activeSessionId} />
              </Panel>
            </>
          )}
        </PanelGroup>
      </Panel>
    </PanelGroup>
  );
}
