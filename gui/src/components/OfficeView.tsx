import React, { useRef } from 'react';
import { Canvas, useFrame } from '@react-three/fiber';
import { OrthographicCamera, OrbitControls, Box, Plane } from '@react-three/drei';
import * as THREE from 'three';

const AgentAvatar = ({ position, color }: { position: [number, number, number], color: string }) => {
  const meshRef = useRef<THREE.Mesh>(null);
  
  // Simple idle animation (bobbing)
  useFrame((state) => {
    if (meshRef.current) {
      meshRef.current.position.y = position[1] + Math.sin(state.clock.elapsedTime * 2) * 0.1;
    }
  });

  return (
    <Box ref={meshRef} position={position} args={[1, 2, 1]} castShadow receiveShadow>
      <meshStandardMaterial color={color} />
    </Box>
  );
};

const Desk = ({ position }: { position: [number, number, number] }) => (
  <Box position={position} args={[2.5, 1, 1.5]} castShadow receiveShadow>
    <meshStandardMaterial color="#8B4513" />
  </Box>
);

export const OfficeView = () => {
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

        {/* Sample Desks and Agents */}
        <Desk position={[-2, -0.5, -2]} />
        <AgentAvatar position={[-2, 0.5, -3]} color="#3b82f6" /> {/* Developer 1 */}

        <Desk position={[3, -0.5, 2]} />
        <AgentAvatar position={[3, 0.5, 1]} color="#10b981" /> {/* Manager */}
        
        <Desk position={[-4, -0.5, 4]} />
        <AgentAvatar position={[-4, 0.5, 3]} color="#ef4444" /> {/* QA */}

      </Canvas>
    </div>
  );
};
