import { useRef } from 'react';
import { Canvas, useFrame } from '@react-three/fiber';
import { OrthographicCamera, OrbitControls, Box, Html } from '@react-three/drei';
import * as THREE from 'three';
import { SessionInfo as ProtoSessionInfo, SessionStatus } from '../gen/localharness/v1/localharness_pb';
import { Space } from '../App';

interface OfficeViewProps {
  sessions: ProtoSessionInfo[];
  spaces?: Space[];
  sessionSpaces?: Record<string, string>;
  onSelectSession: (id: string) => void;
}

const AgentAvatar = ({ position, color, name, status, onClick }: { position: [number, number, number], color: string, name: string, status: SessionStatus, onClick: () => void }) => {
  const meshRef = useRef<THREE.Mesh>(null);
  
  useFrame((state) => {
    if (meshRef.current) {
      const time = state.clock.elapsedTime;
      if (status === SessionStatus.RUNNING) {
        meshRef.current.position.y = position[1] + Math.sin(time * 8) * 0.15;
        meshRef.current.rotation.y = Math.sin(time * 4) * 0.2;
      } else if (status === SessionStatus.ERROR) {
        meshRef.current.position.x = position[0] + Math.sin(time * 20) * 0.1;
        meshRef.current.position.y = position[1];
        meshRef.current.rotation.y = 0;
      } else if (status === SessionStatus.BLOCKED) {
        meshRef.current.position.y = position[1] + Math.sin(time * 1.5) * 0.3;
        meshRef.current.rotation.y = 0;
        meshRef.current.position.x = position[0];
      } else {
        meshRef.current.position.y = position[1] + Math.sin(time * 2) * 0.1;
        meshRef.current.position.x = position[0];
        meshRef.current.rotation.y = 0;
      }
    }
  });

  return (
    <group position={position}>
      <Box ref={meshRef} args={[1, 2, 1]} castShadow receiveShadow onClick={onClick} onPointerOver={() => document.body.style.cursor = 'pointer'} onPointerOut={() => document.body.style.cursor = 'auto'}>
        <meshStandardMaterial color={status === SessionStatus.ERROR ? '#ff0000' : status === SessionStatus.BLOCKED ? '#facc15' : color} />
      </Box>
      <Html position={[0, 1.5, 0]} center>
        <div className="bg-bg-primary text-text-primary px-2 py-1 rounded text-xs font-semibold whitespace-nowrap border border-border-primary shadow-sm pointer-events-none">
          {name || 'Agent'}
        </div>
      </Html>
    </group>
  );
};

const Desk = ({ position }: { position: [number, number, number] }) => (
  <Box position={position} args={[2.5, 1, 1.5]} castShadow receiveShadow>
    <meshStandardMaterial color="#8B4513" />
  </Box>
);

export const OfficeView = ({ sessions = [], spaces = [], sessionSpaces = {}, onSelectSession }: OfficeViewProps) => {
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

  sessions.forEach(session => {
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
        
        <div className="flex justify-between items-end mb-1">
          <div className="text-2xl font-bold text-text-primary">
            ${totalSpent.toFixed(4)}
          </div>
          <div className="text-sm text-text-secondary">
            / ${totalAllocated.toFixed(2)}
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
          onClick={() => alert("Mock: Add Funds to Manager's budget")}
        >
          Deposit Funds
        </button>
      </div>

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

        {/* Map out Spaces and Workspaces */}
        {Object.entries(groupedData).map(([spaceId, workspaces], spaceIndex) => {
          // If space is empty, skip rendering its platform unless we want to show empty spaces
          const workspaceEntries = Object.entries(workspaces);
          if (workspaceEntries.length === 0) return null;

          const spaceOffsetX = spaceIndex * 40 - 20; // Spread spaces out along X axis
          const spaceName = getSpaceName(spaceId);

          return (
            <group key={spaceId} position={[spaceOffsetX, 0, 0]}>
              {/* Space Platform */}
              <Box args={[35, 1, 35]} position={[0, -1, 0]} receiveShadow>
                <meshStandardMaterial color={spaceId === 'unassigned' ? '#e5e7eb' : '#dbeafe'} />
              </Box>
              
              {/* Space Title */}
              <Html position={[0, 0.5, -17]} center>
                <div className="bg-bg-primary/90 text-text-primary px-3 py-1.5 rounded-md text-sm font-bold shadow-sm whitespace-nowrap border border-border-primary">
                  {spaceName}
                </div>
              </Html>

              {/* Map Workspaces within the Space */}
              {workspaceEntries.map(([workspacePath, workspaceSessions], wsIndex) => {
                // Layout workspaces in a grid within the space platform
                const wsCols = 2;
                const wsX = (wsIndex % wsCols) * 16 - 8;
                const wsZ = Math.floor(wsIndex / wsCols) * 16 - 8;

                // Make the path shorter for display
                const shortPath = workspacePath.split('/').pop() || workspacePath;

                return (
                  <group key={workspacePath} position={[wsX, 0, wsZ]}>
                    {/* Workspace Rug/Platform */}
                    <Box args={[14, 0.2, 14]} position={[0, -0.4, 0]} receiveShadow>
                      <meshStandardMaterial color="#cbd5e1" />
                    </Box>

                    {/* Workspace Label */}
                    <Html position={[0, 0.5, -6]} center>
                      <div className="bg-bg-secondary/90 text-text-secondary px-2 py-1 rounded text-[10px] font-mono shadow-sm whitespace-nowrap border border-border-primary">
                        {shortPath}
                      </div>
                    </Html>

                    {/* Agents in this Workspace */}
                    {workspaceSessions.map((session, agentIndex) => {
                      const aCols = 3;
                      const aX = (agentIndex % aCols) * 4 - 4;
                      const aZ = Math.floor(agentIndex / aCols) * 4 - 2;
                      
                      const colors = ['#3b82f6', '#10b981', '#ef4444', '#f59e0b', '#8b5cf6'];
                      const color = colors[session.id.length % colors.length];

                      return (
                        <group key={session.id}>
                          <Desk position={[aX, -0.2, aZ]} />
                          <AgentAvatar 
                            position={[aX, 0.8, aZ - 1]} 
                            color={color} 
                            name={session.name || 'Untitled'} 
                            status={session.status}
                            onClick={() => onSelectSession(session.id)}
                          />
                        </group>
                      );
                    })}
                  </group>
                );
              })}
            </group>
          );
        })}

      </Canvas>
    </div>
  );
};
