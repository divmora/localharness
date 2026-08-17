import { useRef } from 'react';
import { Canvas, useFrame } from '@react-three/fiber';
import { OrthographicCamera, OrbitControls, Box, Html, Billboard, Bounds } from '@react-three/drei';
import * as THREE from 'three';
import { SessionInfo as ProtoSessionInfo, SessionStatus } from '../gen/localharness/v1/localharness_pb';
import { Space } from '../App';
import { DepositModal } from './DepositModal';
import { useState, useEffect } from 'react';
import { invoke } from '@tauri-apps/api/core';
import { useHarness } from '../hooks/useHarness';
import { useAgentConnection } from '../hooks/useAgentConnection';
import { ChatPanel } from './ChatPanel';
import { getAgentSpriteLayers, compositeSpriteSheet } from '../utils/spriteCompositor';

const AgentConnectionWrapper = ({ sessionId }: { sessionId: string }) => {
  useAgentConnection(sessionId);
  return null;
};

export interface OfficeAgent {
  session_id: string;
  office_id: string;
  agent_name: string;
  role_description: string;
  employment_type: string;
  gender: string;
  experience_level: string;
  personality_traits: string;
  current_tasks: number;
  specializations: string;
  visiting_session_id: string | null;
}

interface OfficeViewProps {
  sessions: ProtoSessionInfo[];
  spaces?: Space[];
  sessionSpaces?: Record<string, string>;
  onSelectSession: (id: string) => void;
  activeOfficeId: string;
  managerSessionId?: string;
  onManagerCreated?: (sessionId: string) => void;
}


const Desk = ({ position }: { position: [number, number, number] }) => (
  <Box position={position} args={[2.5, 1, 1.5]} castShadow receiveShadow>
    <meshStandardMaterial color="#8B4513" />
  </Box>
);

const CoffeeMachine = ({ position }: { position: [number, number, number] }) => (
  <group position={position}>
    {/* Base table */}
    <Box args={[3, 1, 2]} position={[0, -0.5, 0]} castShadow receiveShadow>
      <meshStandardMaterial color="#e5e7eb" />
    </Box>
    {/* Machine */}
    <Box args={[1, 1.5, 1]} position={[-0.5, 0.75, 0]} castShadow>
      <meshStandardMaterial color="#1f2937" />
    </Box>
    <Box args={[0.5, 1.2, 0.8]} position={[0.5, 0.6, 0]} castShadow>
      <meshStandardMaterial color="#ef4444" />
    </Box>
    <Html position={[0, 2, 0]} center>
      <div className="bg-bg-primary text-text-primary px-2 py-1 rounded text-xs font-bold shadow-sm whitespace-nowrap border border-border-primary">
        ☕ Coffee Break
      </div>
    </Html>
  </group>
);

interface ProceduralAvatarProps {
  session: ProtoSessionInfo;
  agent: OfficeAgent | undefined;
  homePosition: [number, number, number];
  targetPosition: [number, number, number];
  onClick: () => void;
}

const ProceduralAvatar = ({ session, agent, homePosition, targetPosition, onClick }: ProceduralAvatarProps) => {
  const meshRef = useRef<THREE.Group>(null);
  
  // Animate movement
  useFrame((state, delta) => {
    if (meshRef.current) {
      // Lerp position towards target
      meshRef.current.position.x += (targetPosition[0] - meshRef.current.position.x) * delta * 2;
      meshRef.current.position.z += (targetPosition[2] - meshRef.current.position.z) * delta * 2;
      
      // Bounce animation based on experience
      let bounceSpeed = 3;
      let bounceHeight = 0.2;
      
      if (agent) {
        if (agent.experience_level === 'junior') { bounceSpeed = 6; bounceHeight = 0.4; }
        else if (agent.experience_level === 'senior') { bounceSpeed = 2; bounceHeight = 0.1; }
        else if (agent.experience_level === 'expert') { bounceSpeed = 1; bounceHeight = 0.05; }
      }
      
      // Only bounce if moving or working
      const isMoving = Math.abs(targetPosition[0] - meshRef.current.position.x) > 0.1 || Math.abs(targetPosition[2] - meshRef.current.position.z) > 0.1;
      const isWorking = session.status === SessionStatus.RUNNING;
      
      if (isMoving || isWorking) {
        meshRef.current.position.y = homePosition[1] + Math.sin(state.clock.elapsedTime * bounceSpeed) * bounceHeight;
      } else {
        meshRef.current.position.y += (homePosition[1] - meshRef.current.position.y) * delta * 5;
      }
    }
  });

  // Determine scale and color modifiers
  let scale: [number, number, number] = [1.5, 1.5, 1.5]; // Base scale for 2D sprite
  let opacity = 1;
  let transparent = true;
  
  const [agentTexture, setAgentTexture] = useState<THREE.Texture | null>(null);

  useEffect(() => {
    // Generate the layer configuration for this specific agent
    // Since we don't have the full office object here, we'll randomize country based on ID length
    // in a real setup we'd pass the actual Office.country down.
    const mockCountries = ['USA', 'India', 'Japan', 'China'];
    const mockCountry = mockCountries[(session.id.length) % mockCountries.length];
    
    const layers = getAgentSpriteLayers(
      mockCountry,
      agent?.role_description,
      agent?.gender,
      agent?.employment_type
    );
    
    let isMounted = true;
    compositeSpriteSheet(layers).then(texture => {
      if (isMounted) {
        texture.needsUpdate = true;
        // Temporary: Just show a portion of the sprite sheet if it's a grid (e.g. 1/3 width, 1/2 height)
        texture.repeat.set(1/3, 1/2); 
        
        // Pick a random frame based on agent name or ID to add variety
        const nameHash = agent?.agent_name.length || 0;
        const col = nameHash % 3;
        const row = (nameHash % 2) === 0 ? 0 : 0.5;
        texture.offset.set(col * (1/3), row);
        
        setAgentTexture(texture);
      }
    }).catch(console.error);

    return () => { isMounted = false; };
  }, [agent, session.id]);

  return (
    <group position={homePosition}>
      <Billboard
        ref={meshRef}
        follow={true}
        lockX={false}
        lockY={false}
        lockZ={false}
      >
        <mesh onClick={onClick} onPointerOver={() => document.body.style.cursor = 'pointer'} onPointerOut={() => document.body.style.cursor = 'auto'}>
          <planeGeometry args={[scale[0], scale[1]]} />
          <meshStandardMaterial 
            map={agentTexture}
            transparent={transparent}
            opacity={agentTexture ? opacity : 0} // Hide until texture loads
            alphaTest={0.1}
            side={THREE.DoubleSide}
            color={session.status === SessionStatus.ERROR ? '#ffaaaa' : session.status === SessionStatus.BLOCKED ? '#ffffaa' : '#ffffff'}
          />
        </mesh>
      </Billboard>
      <Html position={[0, scale[1] / 2 + 0.2, 0]} center>
        <div className="bg-bg-primary text-text-primary px-2 py-1 rounded text-xs font-semibold whitespace-nowrap border border-border-primary shadow-sm pointer-events-none flex flex-col items-center">
          <span>{agent?.agent_name || session.name || 'Agent'}</span>
          {agent && <span className="text-[9px] text-text-tertiary">{agent.role_description}</span>}
          {agent && agent.current_tasks > 0 && <span className="text-[9px] text-blue-500">{agent.current_tasks} tasks</span>}
        </div>
      </Html>
    </group>
  );
};

export const OfficeView = ({ sessions = [], spaces = [], sessionSpaces = {}, onSelectSession, activeOfficeId, managerSessionId, onManagerCreated }: OfficeViewProps) => {
  const [showDepositModal, setShowDepositModal] = useState(false);
  const [walletBalance, setWalletBalance] = useState<number>(0);
  const [isChatOpen, setIsChatOpen] = useState(true);
  const [officeAgents, setOfficeAgents] = useState<OfficeAgent[]>([]);

  const { connected, steps, sendPrompt, interrupt } = useHarness(managerSessionId || null, null, 0, true, onManagerCreated, activeOfficeId);

  useEffect(() => {
    async function loadData() {
      try {
        const bal = await invoke<number>('get_wallet_balance', { officeId: activeOfficeId });
        setWalletBalance(bal);
        
        const agents = await invoke<OfficeAgent[]>('get_office_agents', { officeId: activeOfficeId });
        setOfficeAgents(agents);
      } catch (err) {
        console.error("Failed to load office data:", err);
      }
    }
    loadData();
    
    // Poll for updates to agents (visiting_session_id, tasks, etc)
    const interval = setInterval(loadData, 2000);
    return () => clearInterval(interval);
  }, [activeOfficeId]);

  const totalAllocated = sessions.reduce((acc, s) => acc + (s.budgetAllocated || 0), 0);
  const totalSpent = sessions.reduce((acc, s) => acc + (s.budgetSpent || 0), 0);
  const percentSpent = totalAllocated > 0 ? (totalSpent / totalAllocated) * 100 : 0;

  // 1. Group sessions into Spaces -> Workspaces
  // SpaceId -> WorkspacePath -> Session[]
  const groupedData: Record<string, Record<string, typeof sessions>> = {};
  
  // Initialize with known spaces
  spaces.forEach(space => {
    groupedData[space.id] = {};
  });
  // Ensure unassigned space exists
  groupedData['unassigned'] = {};

  // Filter sessions to only those in the current office
  const officeSessions = sessions.filter(session => {
    return session.id === managerSessionId || 
           officeAgents.some(a => a.session_id === session.id) ||
           !!sessionSpaces[session.id];
  });

  officeSessions.forEach(session => {
    const spaceId = sessionSpaces[session.id] || 'unassigned';
    const workspacePath = session.workspace || 'No Workspace';
    
    if (!groupedData[spaceId]) {
      groupedData[spaceId] = {};
    }
    if (!groupedData[spaceId][workspacePath]) {
      groupedData[spaceId][workspacePath] = [];
    }
    groupedData[spaceId][workspacePath].push(session);
  });

  const getSpaceName = (spaceId: string) => {
    if (spaceId === 'unassigned') return 'Unassigned Agents';
    return spaces.find(s => s.id === spaceId)?.name || 'Unknown Space';
  };

  return (
    <div className="w-full h-full bg-bg-secondary relative" style={{ minHeight: '400px' }}>
      
      {/* Budget Dashboard Overlay */}
      <div className="absolute top-4 right-4 z-10 bg-bg-primary border border-border-primary rounded-lg p-4 shadow-lg min-w-[250px]">
        <h3 className="text-sm font-bold text-text-primary mb-2 flex justify-between">
          <span>Economy & Budget</span>
          <span className="text-xs font-normal text-text-secondary">Manager Persona</span>
        </h3>
        
        <div className="mb-3">
          <div className="flex justify-between items-end mb-1">
            <div className="text-2xl font-bold text-text-primary">
              {walletBalance.toFixed(0)} <span className="text-sm font-normal text-text-secondary">DC</span>
            </div>
            <div className="text-xs text-text-tertiary uppercase font-bold tracking-wider">
              Available
            </div>
          </div>
        </div>

        <div className="flex justify-between items-end mb-1">
          <div className="text-sm font-bold text-text-primary">
            {totalSpent.toFixed(2)} DC spent
          </div>
          <div className="text-xs text-text-secondary">
            / {totalAllocated.toFixed(0)} DC allocated
          </div>
        </div>

        <div className="w-full bg-bg-secondary rounded-full h-2 mb-3 overflow-hidden border border-border-primary">
          <div 
            className="bg-blue-500 h-2 rounded-full transition-all duration-500" 
            style={{ width: `${Math.min(100, percentSpent)}%`, backgroundColor: percentSpent > 90 ? '#ef4444' : '#3b82f6' }}
          ></div>
        </div>

        <button 
          className="w-full text-xs font-semibold py-1.5 rounded bg-blue-600 hover:bg-blue-700 text-white transition-colors cursor-pointer"
          onClick={() => setShowDepositModal(true)}
        >
          Deposit Funds
        </button>
      </div>

      {/* Manager Chat Overlay */}
      <div 
        className={`absolute top-4 bottom-4 left-4 z-10 w-96 bg-bg-primary/95 backdrop-blur-md border border-border-primary rounded-lg shadow-xl flex flex-col overflow-hidden transition-transform duration-300 ${isChatOpen ? 'translate-x-0' : '-translate-x-[110%]'}`}
      >
        <div className="flex items-center justify-between p-3 border-b border-border-primary bg-bg-secondary shrink-0">
          <div className="font-bold text-sm text-text-primary flex items-center gap-2">
            <span className="w-2 h-2 rounded-full bg-blue-500"></span>
            Office Manager
          </div>
          <button onClick={() => setIsChatOpen(false)} className="text-text-tertiary hover:text-text-primary">
            ✕
          </button>
        </div>
        <div className="flex-1 min-h-0 relative">
          <ChatPanel 
            steps={steps}
            connected={connected}
            onSend={(p: string) => sendPrompt(p)}
            onInterrupt={interrupt}
          />
        </div>
      </div>

      {/* Toggle Chat Button */}
      {!isChatOpen && (
        <button 
          onClick={() => setIsChatOpen(true)}
          className="absolute top-4 left-4 z-10 bg-bg-primary border border-border-primary rounded-lg p-3 shadow-lg hover:bg-bg-secondary transition-colors"
          title="Open Manager Chat"
        >
          <div className="w-2 h-2 rounded-full bg-blue-500 mb-1"></div>
          <div className="w-4 h-0.5 bg-text-tertiary"></div>
        </button>
      )}

      {showDepositModal && (
        <DepositModal 
          officeId={activeOfficeId}
          currentBalance={walletBalance} 
          onClose={() => setShowDepositModal(false)} 
          onDepositComplete={setWalletBalance}
        />
      )}

      <Canvas shadows>
        {/* Isometric Orthographic Camera */}
        <OrthographicCamera 
          makeDefault 
          position={[20, 20, 20]} 
          zoom={40} 
          near={-100} 
          far={100}
        />
        
        {/* Controls so the user can pan around the office */}
        <OrbitControls 
          makeDefault
          enableRotate={false} /* Lock rotation to keep isometric perspective */
          enableZoom={true}
          enablePan={true}
          target={[0, 0, 0]}
        />

        {/* Lighting */}
        <ambientLight intensity={0.5} />
        <directionalLight 
          position={[10, 20, 10]} 
          intensity={1} 
          castShadow 
          shadow-mapSize={[1024, 1024]}
        />
        
        <Bounds fit clip observe margin={1.2}>
          {/* Map out Spaces and Workspaces */}
          {(() => {
          // Pre-calculate all desk world positions
          const worldDeskPositions: Record<string, [number, number, number]> = {};
          
          Object.entries(groupedData).forEach(([_, workspaces], spaceIndex) => {
            const workspaceEntries = Object.entries(workspaces);
            if (workspaceEntries.length === 0) return;
            const spaceOffsetX = spaceIndex * 40 - 20;

            workspaceEntries.forEach(([_, workspaceSessions], wsIndex) => {
              const wsCols = 2;
              const wsX = spaceOffsetX + (wsIndex % wsCols) * 16 - 8;
              const wsZ = Math.floor(wsIndex / wsCols) * 16 - 8;

              workspaceSessions.forEach((session, agentIndex) => {
                const aCols = 3;
                const aX = wsX + (agentIndex % aCols) * 4 - 4;
                const aZ = wsZ + Math.floor(agentIndex / aCols) * 4 - 2;
                worldDeskPositions[session.id] = [aX, 0, aZ];
              });
            });
          });

          const COFFEE_MACHINE_POS: [number, number, number] = [0, 0, -25];

          return (
            <>
              <CoffeeMachine position={COFFEE_MACHINE_POS} />

              {/* Render Spaces & Desks */}
              {Object.entries(groupedData).map(([spaceId, workspaces], spaceIndex) => {
                const workspaceEntries = Object.entries(workspaces);
                if (workspaceEntries.length === 0) return null;

                const spaceOffsetX = spaceIndex * 40 - 20;
                const spaceName = getSpaceName(spaceId);

                return (
                  <group key={spaceId} position={[spaceOffsetX, 0, 0]}>
                    <Box args={[35, 1, 35]} position={[0, -1, 0]} receiveShadow>
                      <meshStandardMaterial color={spaceId === 'unassigned' ? '#e5e7eb' : '#dbeafe'} />
                    </Box>
                    <Html position={[0, 0.5, -17]} center>
                      <div className="bg-bg-primary/90 text-text-primary px-3 py-1.5 rounded-md text-sm font-bold shadow-sm whitespace-nowrap border border-border-primary">
                        {spaceName}
                      </div>
                    </Html>

                    {workspaceEntries.map(([workspacePath, workspaceSessions], wsIndex) => {
                      const wsCols = 2;
                      const wsX = (wsIndex % wsCols) * 16 - 8;
                      const wsZ = Math.floor(wsIndex / wsCols) * 16 - 8;
                      const shortPath = workspacePath.split('/').pop() || workspacePath;

                      return (
                        <group key={workspacePath} position={[wsX, 0, wsZ]}>
                          <Box args={[14, 0.2, 14]} position={[0, -0.4, 0]} receiveShadow>
                            <meshStandardMaterial color="#cbd5e1" />
                          </Box>
                          <Html position={[0, 0.5, -6]} center>
                            <div className="bg-bg-secondary/90 text-text-secondary px-2 py-1 rounded text-[10px] font-mono shadow-sm whitespace-nowrap border border-border-primary">
                              {shortPath}
                            </div>
                          </Html>

                          {workspaceSessions.map((session, agentIndex) => {
                            const aCols = 3;
                            const aX = (agentIndex % aCols) * 4 - 4;
                            const aZ = Math.floor(agentIndex / aCols) * 4 - 2;
                            return <Desk key={`desk-${session.id}`} position={[aX, -0.2, aZ]} />;
                          })}
                        </group>
                      );
                    })}
                  </group>
                );
              })}

              {/* Render Avatars at Root */}
              {sessions.map(session => {
                const agent = officeAgents.find(a => a.session_id === session.id);
                const deskPos = worldDeskPositions[session.id];
                if (!deskPos) return null;

                // Avatar base position is slightly above desk and behind it
                const homePos: [number, number, number] = [deskPos[0], 0.8, deskPos[2] - 1];
                let targetPos = [...homePos] as [number, number, number];

                // Check visiting session
                if (agent?.visiting_session_id) {
                  const targetDesk = worldDeskPositions[agent.visiting_session_id];
                  if (targetDesk) {
                    targetPos = [targetDesk[0], 0.8, targetDesk[2] - 1]; // Go to their desk
                  }
                } 
                // Idle chit-chat pathing (randomly go to coffee machine if idle and no tasks)
                else if (agent && agent.current_tasks === 0 && session.status === SessionStatus.READY) {
                  // Use session ID to create a stable pseudo-random boolean so they don't jitter
                  // If hash ends in some digit, they go to coffee machine
                  const charCode = session.id.charCodeAt(session.id.length - 1);
                  // Make it time-based so they go back and forth
                  const timeCycle = Math.floor(Date.now() / 15000); // 15 sec cycle
                  if ((charCode + timeCycle) % 3 === 0) {
                    // Spread them out around the coffee machine
                    const offsetX = (charCode % 5) - 2;
                    const offsetZ = ((charCode * 2) % 3);
                    targetPos = [COFFEE_MACHINE_POS[0] + offsetX, 0.8, COFFEE_MACHINE_POS[2] + 2 + offsetZ];
                  }
                }

                return (
                  <group key={session.id}>
                    <AgentConnectionWrapper sessionId={session.id} />
                    <ProceduralAvatar 
                      session={session}
                      agent={agent}
                      homePosition={homePos}
                      targetPosition={targetPos}
                      onClick={() => onSelectSession(session.id)}
                    />
                  </group>
                );
              })}
            </>
          );
        })()}
        </Bounds>
      </Canvas>
    </div>
  );
};
