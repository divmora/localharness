import { Panel, Group as PanelGroup, Separator as PanelResizeHandle } from 'react-resizable-panels';
import { AgentSidebar } from '../components/AgentSidebar';
import { ChatPanel } from '../components/ChatPanel';
import { WorkspacePanel } from '../components/WorkspacePanel';
import { TerminalPanel } from '../components/TerminalPanel';
import { SessionInfo as ProtoSessionInfo, StepUpdate, TrajectoryState_TrajState } from '../gen/localharness/v1/localharness_pb';
import { Space } from '../App';
import { CenteredEmptyState } from '../components/CenteredEmptyState';

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
  trajectoryState,
  onInterrupt,
  onResume,
  showTerminal = true
}: MainPageProps) {
  return (
    // @ts-ignore
    <PanelGroup autoSaveId="harness-main-layout" orientation="horizontal" className="flex-1">
      {/* Left Pane: Agent Sidebar */}
      {showAgentSidebar && (
        <>
          <Panel defaultSize={20} minSize={10} className="flex flex-col" collapsible>
            <AgentSidebar
              activeSessionId={activeSessionId}
              onSelectSession={onSelectSession}
              onNewSession={onNewSession}
              onCreateSpace={onCreateSpace}
              onMoveSessionToSpace={onMoveSessionToSpace}
              onOpenCustomizations={onOpenCustomizations}
              onOpenSessionsManager={onOpenSessionsManager}
              onDeleteSession={onDeleteSession}
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
        {/* @ts-ignore */}
        <PanelGroup autoSaveId="harness-vertical-layout" orientation="vertical">
          {/* Top Section: Chat + Editor */}
          <Panel defaultSize={70} minSize={20} className="flex flex-col">
            {/* @ts-ignore */}
            <PanelGroup autoSaveId="harness-chat-editor-layout" orientation="horizontal">
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
                  <PanelResizeHandle className="w-[1px] bg-border-primary transition-colors hover:opacity-80" style={{ backgroundColor: 'var(--border-primary)' }} />

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
              <PanelResizeHandle
                className="h-[1px] bg-border-primary transition-colors hover:opacity-80"
              />

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
