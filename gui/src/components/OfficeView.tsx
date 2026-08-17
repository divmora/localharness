import { useRef } from 'react';
import { Canvas, useFrame } from '@react-three/fiber';
import { OrthographicCamera, OrbitControls, Box, Plane, Html } from '@react-three/drei';
import * as THREE from 'three';
import { SessionInfo as ProtoSessionInfo, SessionStatus } from '../gen/localharness/v1/localharness_pb';

interface OfficeViewProps {
  sessions: ProtoSessionInfo[];
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

export const OfficeView = ({ sessions = [], onSelectSession }: OfficeViewProps) => {
  return (
    <div className="w-full h-full bg-bg-secondary relative" style={{ minHeight: '400px' }}>
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

        {/* Floor (The Office Grid) */}
        <Plane 
          args={[50, 50]} 
          rotation={[-Math.PI / 2, 0, 0]} 
          position={[0, -1, 0]} 
          receiveShadow
        >
          <meshStandardMaterial color="#ddd" />
          <gridHelper args={[50, 50, '#999', '#ccc']} rotation={[Math.PI / 2, 0, 0]} />
        </Plane>

        {/* Map real sessions */}
        {sessions.map((session, index) => {
          // Simple grid layout
          const cols = 4;
          const x = (index % cols) * 5 - 7.5;
          const z = Math.floor(index / cols) * 5 - 7.5;
          
          // Pick a random color based on ID length or just use blue
          const colors = ['#3b82f6', '#10b981', '#ef4444', '#f59e0b', '#8b5cf6'];
          const color = colors[session.id.length % colors.length];

          return (
            <group key={session.id}>
              <Desk position={[x, -0.5, z]} />
              <AgentAvatar 
                position={[x, 0.5, z - 1]} 
                color={color} 
                name={session.name || 'Untitled Agent'} 
                status={session.status}
                onClick={() => onSelectSession(session.id)}
              />
            </group>
          );
        })}

      </Canvas>
    </div>
  );
};
