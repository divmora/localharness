import { useRef, useState, useEffect } from 'react';
import { Canvas, useFrame } from '@react-three/fiber';
import { OrthographicCamera, PerspectiveCamera, OrbitControls, Box, Html, Bounds, RoundedBox, Sphere, Cylinder } from '@react-three/drei';
import * as THREE from 'three';
import { motion } from 'framer-motion';
import { SessionInfo as ProtoSessionInfo, SessionStatus } from '../gen/localharness/v1/localharness_pb';
import { Space } from '../App';
import { HireAgentModal } from './HireAgentModal';
import { invoke } from '@tauri-apps/api/core';
import { useHarness } from '../hooks/useHarness';
import { useAgentConnection } from '../hooks/useAgentConnection';
import { ChatPanel } from './ChatPanel';
import { findPath } from '../utils/pathfinding';

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
  activeOfficeId?: string;
  managerSessionId?: string;
  onManagerCreated?: (sessionId: string) => void;
  workspacePath?: string | null;
  onRefreshSessions?: () => void;
}

const Desk = ({ position, rotation = [0, 0, 0] }: { position: [number, number, number], rotation?: [number, number, number] }) => (
  <group position={position} rotation={rotation}>
    {/* Desk Top */}
    <RoundedBox args={[2.5, 0.1, 1.5]} position={[0, 0.5, 0]} radius={0.05} castShadow receiveShadow>
      <meshStandardMaterial color="#8B4513" roughness={0.7} />
    </RoundedBox>
    {/* Legs */}
    <Cylinder args={[0.05, 0.05, 1]} position={[-1.1, 0, -0.6]} castShadow><meshStandardMaterial color="#333" /></Cylinder>
    <Cylinder args={[0.05, 0.05, 1]} position={[1.1, 0, -0.6]} castShadow><meshStandardMaterial color="#333" /></Cylinder>
    <Cylinder args={[0.05, 0.05, 1]} position={[-1.1, 0, 0.6]} castShadow><meshStandardMaterial color="#333" /></Cylinder>
    <Cylinder args={[0.05, 0.05, 1]} position={[1.1, 0, 0.6]} castShadow><meshStandardMaterial color="#333" /></Cylinder>
    {/* Monitor */}
    <RoundedBox args={[1.2, 0.8, 0.1]} position={[0, 1, -0.4]} radius={0.02} castShadow><meshStandardMaterial color="#111" /></RoundedBox>
    {/* Monitor Screen (glow) */}
    <Box args={[1.1, 0.7, 0.05]} position={[0, 1, -0.37]}><meshBasicMaterial color="#3b82f6" /></Box>
    {/* Monitor Stand */}
    <Cylinder args={[0.05, 0.08, 0.4]} position={[0, 0.7, -0.4]} castShadow><meshStandardMaterial color="#222" /></Cylinder>
    <Box args={[0.4, 0.02, 0.3]} position={[0, 0.52, -0.4]} castShadow><meshStandardMaterial color="#222" /></Box>
    {/* Keyboard */}
    <RoundedBox args={[0.8, 0.05, 0.3]} position={[0, 0.55, 0.2]} radius={0.01} castShadow><meshStandardMaterial color="#ccc" /></RoundedBox>
    {/* Chair */}
    <group position={[0, 0, 0.8]}>
      <RoundedBox args={[0.8, 0.1, 0.8]} position={[0, 0.3, 0]} radius={0.05} castShadow><meshStandardMaterial color="#444" /></RoundedBox>
      <RoundedBox args={[0.8, 0.8, 0.1]} position={[0, 0.7, 0.35]} radius={0.05} castShadow><meshStandardMaterial color="#444" /></RoundedBox>
      <Cylinder args={[0.05, 0.05, 0.3]} position={[0, 0.15, 0]} castShadow><meshStandardMaterial color="#222" /></Cylinder>
    </group>
  </group>
);

const PodPlant = ({ position }: { position: [number, number, number] }) => (
  <group position={position}>
    <Cylinder args={[0.4, 0.3, 0.6]} position={[0, 0.3, 0]} castShadow><meshStandardMaterial color="#e5e7eb" /></Cylinder>
    <Sphere args={[0.6]} position={[0, 0.9, 0]} castShadow><meshStandardMaterial color="#22c55e" roughness={0.9} /></Sphere>
    <Sphere args={[0.4]} position={[0.3, 1.1, 0.2]} castShadow><meshStandardMaterial color="#16a34a" roughness={0.9} /></Sphere>
    <Sphere args={[0.5]} position={[-0.2, 0.8, 0.4]} castShadow><meshStandardMaterial color="#15803d" roughness={0.9} /></Sphere>
  </group>
);

const ManagerDesk = ({ position }: { position: [number, number, number] }) => (
  <group position={position}>
    {/* Raised Platform */}
    <RoundedBox args={[8, 0.4, 8]} position={[0, -0.2, 0]} radius={0.1} receiveShadow castShadow>
      <meshStandardMaterial color="#94a3b8" />
    </RoundedBox>
    {/* Glass Walls */}
    <Box args={[8, 4, 0.1]} position={[0, 2, -4]}><meshStandardMaterial color="#e0f2fe" opacity={0.3} transparent /></Box>
    <Box args={[0.1, 4, 8]} position={[-4, 2, 0]}><meshStandardMaterial color="#e0f2fe" opacity={0.3} transparent /></Box>
    
    {/* Big Executive Desk Top */}
    <RoundedBox args={[4.0, 0.2, 2.2]} position={[0, 0.7, 0]} radius={0.05} castShadow receiveShadow>
      <meshStandardMaterial color="#3f2e1a" />
    </RoundedBox>
    {/* Desk Legs */}
    <Cylinder args={[0.1, 0.1, 1.2]} position={[-1.5, 0.1, -0.8]} castShadow><meshStandardMaterial color="#111" /></Cylinder>
    <Cylinder args={[0.1, 0.1, 1.2]} position={[1.5, 0.1, -0.8]} castShadow><meshStandardMaterial color="#111" /></Cylinder>
    <Cylinder args={[0.1, 0.1, 1.2]} position={[-1.5, 0.1, 0.8]} castShadow><meshStandardMaterial color="#111" /></Cylinder>
    <Cylinder args={[0.1, 0.1, 1.2]} position={[1.5, 0.1, 0.8]} castShadow><meshStandardMaterial color="#111" /></Cylinder>
    {/* Ultra Wide Monitor */}
    <RoundedBox args={[2.0, 0.9, 0.1]} position={[0, 1.3, -0.6]} radius={0.02} castShadow><meshStandardMaterial color="#000" /></RoundedBox>
    {/* Monitor Screen */}
    <Box args={[1.9, 0.8, 0.05]} position={[0, 1.3, -0.57]}><meshBasicMaterial color="#38bdf8" /></Box>
    {/* Monitor Stand */}
    <Cylinder args={[0.08, 0.1, 0.5]} position={[0, 0.9, -0.6]} castShadow><meshStandardMaterial color="#222" /></Cylinder>
    {/* Keyboard */}
    <RoundedBox args={[1.0, 0.05, 0.4]} position={[0, 0.8, 0.3]} radius={0.01} castShadow><meshStandardMaterial color="#999" /></RoundedBox>
    {/* Executive Chair */}
    <group position={[0, 0, 1.2]}>
      <RoundedBox args={[1, 0.2, 1]} position={[0, 0.4, 0]} radius={0.05} castShadow><meshStandardMaterial color="#111" /></RoundedBox>
      <RoundedBox args={[1, 1.2, 0.2]} position={[0, 1.0, 0.4]} radius={0.05} castShadow><meshStandardMaterial color="#111" /></RoundedBox>
      <Cylinder args={[0.08, 0.08, 0.4]} position={[0, 0.2, 0]} castShadow><meshStandardMaterial color="#333" /></Cylinder>
    </group>
  </group>
);

const Wall = ({ position, args, rotation = [0, 0, 0] }: { position: [number, number, number], args: [number, number, number], rotation?: [number, number, number] }) => (
  <Box args={args} position={position} rotation={rotation} castShadow receiveShadow>
    <meshStandardMaterial color="#94a3b8" />
  </Box>
);

const GlassWall = ({ position, args, rotation = [0, 0, 0] }: { position: [number, number, number], args: [number, number, number], rotation?: [number, number, number] }) => (
  <Box args={args} position={position} rotation={rotation}>
    <meshStandardMaterial color="#e0f2fe" opacity={0.3} transparent />
  </Box>
);

const CoffeeMachine = ({ position }: { position: [number, number, number] }) => {
  const [brewing, setBrewing] = useState(false);
  
  const handleClick = (e: any) => {
    e.stopPropagation();
    if (brewing) return;
    setBrewing(true);
    setTimeout(() => setBrewing(false), 3000);
  };

  return (
    <group position={position} onClick={handleClick} onPointerOver={(e) => { e.stopPropagation(); document.body.style.cursor = 'pointer' }} onPointerOut={() => document.body.style.cursor = 'auto'}>
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
        <div className={`px-2 py-1 rounded text-xs font-bold shadow-sm whitespace-nowrap border ${brewing ? 'bg-amber-100 text-amber-900 border-amber-300' : 'bg-bg-primary text-text-primary border-border-primary'}`}>
          {brewing ? '☕ Brewing...' : '☕ Coffee Machine'}
        </div>
      </Html>
    </group>
  );
};

const Breakroom = ({ position }: { position: [number, number, number] }) => (
  <group position={position}>
    {/* Floor */}
    <Box args={[12, 0.1, 12]} position={[0, -0.45, 0]} receiveShadow>
       <meshStandardMaterial color="#fca5a5" />
    </Box>
    {/* Walls */}
    <Wall args={[12, 3, 0.2]} position={[0, 1, -6]} />
    <Wall args={[0.2, 3, 12]} position={[-6, 1, 0]} />
    {/* Partial front wall */}
    <Wall args={[4, 3, 0.2]} position={[-4, 1, 6]} />
    <Wall args={[4, 3, 0.2]} position={[4, 1, 6]} />
    <CoffeeMachine position={[0, 0, -2]} />
    
    <Html position={[0, 3, 0]} center>
      <div className="bg-bg-primary text-text-primary px-2 py-1 rounded text-xs font-bold shadow-sm whitespace-nowrap border border-border-primary">
        ☕ Breakroom
      </div>
    </Html>
  </group>
);

const MeetingRoom = ({ position }: { position: [number, number, number] }) => (
  <group position={position}>
    {/* Floor */}
    <Box args={[14, 0.1, 10]} position={[0, -0.45, 0]} receiveShadow>
       <meshStandardMaterial color="#fef08a" />
    </Box>
    {/* Glass Walls */}
    <GlassWall args={[14, 4, 0.1]} position={[0, 1.5, -5]} />
    <GlassWall args={[14, 4, 0.1]} position={[0, 1.5, 5]} />
    <GlassWall args={[0.1, 4, 10]} position={[-7, 1.5, 0]} />
    {/* Right wall has a door hole, so two smaller walls */}
    <GlassWall args={[0.1, 4, 3]} position={[7, 1.5, -3.5]} />
    <GlassWall args={[0.1, 4, 3]} position={[7, 1.5, 3.5]} />
    
    {/* Conference Table */}
    <RoundedBox args={[8, 0.2, 3]} position={[0, 0.6, 0]} radius={0.05} castShadow receiveShadow>
       <meshStandardMaterial color="#3f2e1a" />
    </RoundedBox>
    <Cylinder args={[0.2, 0.2, 1]} position={[-3, 0.1, 0]} castShadow><meshStandardMaterial color="#111" /></Cylinder>
    <Cylinder args={[0.2, 0.2, 1]} position={[3, 0.1, 0]} castShadow><meshStandardMaterial color="#111" /></Cylinder>
    
    <Html position={[0, 4, 0]} center>
      <div className="bg-bg-primary text-text-primary px-2 py-1 rounded text-xs font-bold shadow-sm whitespace-nowrap border border-border-primary">
        🤝 Conference Room A
      </div>
    </Html>
  </group>
);


interface ProceduralAvatarProps {
  session: ProtoSessionInfo;
  agent: OfficeAgent | undefined;
  homePosition: [number, number, number];
  targetPosition: [number, number, number];
  rotation?: [number, number, number];
  onClick: () => void;
  isWalkable: (x: number, z: number) => boolean;
  isFollowed: boolean;
  onFollowToggle: () => void;
}

const ProceduralAvatar = ({ session, agent, homePosition, targetPosition, rotation, onClick, isWalkable, isFollowed, onFollowToggle }: ProceduralAvatarProps) => {
  const meshRef = useRef<THREE.Group>(null);
  const armLRef = useRef<THREE.Group>(null);
  const armRRef = useRef<THREE.Group>(null);
  const legLRef = useRef<THREE.Group>(null);
  const legRRef = useRef<THREE.Group>(null);
  const groupRef = useRef<THREE.Group>(null);
  
  const [path, setPath] = useState<[number, number][]>([]);
  const [thought, setThought] = useState('Working');

  // Web Audio Context for Sound Effects
  const audioCtxRef = useRef<AudioContext | null>(null);
  const lastFootstepRef = useRef<number>(0);
  const lastTypeRef = useRef<number>(0);

  const playClickSound = () => {
    if (!audioCtxRef.current) audioCtxRef.current = new (window.AudioContext || (window as any).webkitAudioContext)();
    if (audioCtxRef.current.state === 'suspended') audioCtxRef.current.resume();
    
    const osc = audioCtxRef.current.createOscillator();
    const gain = audioCtxRef.current.createGain();
    osc.type = 'square';
    osc.frequency.setValueAtTime(400 + Math.random() * 400, audioCtxRef.current.currentTime);
    gain.gain.setValueAtTime(0.01 + Math.random() * 0.02, audioCtxRef.current.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.001, audioCtxRef.current.currentTime + 0.03);
    osc.connect(gain);
    gain.connect(audioCtxRef.current.destination);
    osc.start();
    osc.stop(audioCtxRef.current.currentTime + 0.03);
  };

  const playFootstepSound = () => {
    if (!audioCtxRef.current) audioCtxRef.current = new (window.AudioContext || (window as any).webkitAudioContext)();
    if (audioCtxRef.current.state === 'suspended') audioCtxRef.current.resume();
    
    const osc = audioCtxRef.current.createOscillator();
    const gain = audioCtxRef.current.createGain();
    osc.type = 'triangle';
    osc.frequency.setValueAtTime(150 + Math.random() * 50, audioCtxRef.current.currentTime);
    gain.gain.setValueAtTime(0.05, audioCtxRef.current.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.001, audioCtxRef.current.currentTime + 0.05);
    osc.connect(gain);
    gain.connect(audioCtxRef.current.destination);
    osc.start();
    osc.stop(audioCtxRef.current.currentTime + 0.05);
  };

  // Dynamic Thoughts
  useEffect(() => {
    if (session.status === SessionStatus.RUNNING) {
      const thoughts = ['Writing code...', 'Debugging...', 'Compiling...', 'Thinking...', 'Searching files...', 'Reading logs...', 'Where did the memory leak go?', 'Just one more fix...'];
      setThought(thoughts[Math.floor(Math.random() * thoughts.length)]);
      
      const interval = setInterval(() => {
        setThought(thoughts[Math.floor(Math.random() * thoughts.length)]);
      }, 4000 + Math.random() * 2000);
      
      return () => clearInterval(interval);
    }
  }, [session.status]);

  // When target changes, compute new path from current physical position
  useEffect(() => {
    if (groupRef.current) {
      const currentX = groupRef.current.position.x;
      const currentZ = groupRef.current.position.z;
      const newPath = findPath(currentX, currentZ, targetPosition[0], targetPosition[2], isWalkable);
      setPath(newPath);
    }
  }, [targetPosition[0], targetPosition[2]]);

  // Styling based on agent
  let skinColor = '#fcd34d'; // default yellow-ish
  let shirtColor = '#3b82f6'; // default blue
  let pantsColor = '#1f2937'; // default dark gray

  if (agent) {
    if (agent.role_description.toLowerCase().includes('engineer')) shirtColor = '#ef4444'; // red
    else if (agent.role_description.toLowerCase().includes('design')) shirtColor = '#a855f7'; // purple
    else if (agent.role_description.toLowerCase().includes('product')) shirtColor = '#22c55e'; // green
    
    if (agent.gender === 'female') {
      skinColor = '#fde047';
      pantsColor = '#4b5563';
    }
  }
  
  if (session.id.includes('manager')) {
    shirtColor = '#111827'; // Black suit
    pantsColor = '#000000';
  }

  useFrame((state, delta) => {
    if (!meshRef.current || !groupRef.current) return;
    
    const isWorking = session.status === SessionStatus.RUNNING;
    let isMoving = false;
    
    // Follow path
    if (path.length > 0) {
      const nextWaypoint = path[0];
      const dx = nextWaypoint[0] - groupRef.current.position.x;
      const dz = nextWaypoint[1] - groupRef.current.position.z;
      const distance = Math.sqrt(dx * dx + dz * dz);
      
      if (distance < 0.2) {
        // Reached waypoint, pop it
        setPath(prev => prev.slice(1));
      } else {
        isMoving = true;
        const speed = 6;
        groupRef.current.position.x += (dx / distance) * speed * delta;
        groupRef.current.position.z += (dz / distance) * speed * delta;
        
        // Face movement direction
        const angle = Math.atan2(dx, dz);
        groupRef.current.rotation.y = angle;
      }
    } else {
      // Exactly at destination or last waypoint
      const dx = targetPosition[0] - groupRef.current.position.x;
      const dz = targetPosition[2] - groupRef.current.position.z;
      const distance = Math.sqrt(dx * dx + dz * dz);
      
      if (distance > 0.05) {
        // Move towards exact target
        isMoving = true;
        const speed = 6;
        groupRef.current.position.x += (dx / distance) * speed * delta;
        groupRef.current.position.z += (dz / distance) * speed * delta;
        const angle = Math.atan2(dx, dz);
        groupRef.current.rotation.y = angle;
      } else {
        // Snap to exact
        groupRef.current.position.x = targetPosition[0];
        groupRef.current.position.z = targetPosition[2];
        groupRef.current.rotation.y = rotation ? rotation[1] : 0;
      }
    }

    // Kinematics (Animation)
    const time = state.clock.elapsedTime;
    
    if (armLRef.current && armRRef.current && legLRef.current && legRRef.current) {
      if (isMoving) {
        // Walk cycle
        const walkSpeed = 15;
        armLRef.current.rotation.x = Math.sin(time * walkSpeed) * 0.5;
        armRRef.current.rotation.x = -Math.sin(time * walkSpeed) * 0.5;
        legLRef.current.rotation.x = -Math.sin(time * walkSpeed) * 0.5;
        legRRef.current.rotation.x = Math.sin(time * walkSpeed) * 0.5;
        
        // Bob up and down
        meshRef.current.position.y = Math.abs(Math.sin(time * walkSpeed)) * 0.1;

        if (isFollowed && time - lastFootstepRef.current > 0.3) {
          playFootstepSound();
          lastFootstepRef.current = time;
        }
      } else if (isWorking) {
        // Type cycle
        const typeSpeed = 20;
        armLRef.current.rotation.x = -0.5 + Math.sin(time * typeSpeed) * 0.2;
        armRRef.current.rotation.x = -0.5 + Math.cos(time * typeSpeed * 1.1) * 0.2;
        legLRef.current.rotation.x = 0;
        legRRef.current.rotation.x = 0;
        meshRef.current.position.y = 0;

        if (isFollowed && time - lastTypeRef.current > 0.1) {
          if (Math.random() > 0.3) playClickSound();
          lastTypeRef.current = time;
        }
      } else {
        // Idle
        armLRef.current.rotation.x = 0;
        armRRef.current.rotation.x = 0;
        legLRef.current.rotation.x = 0;
        legRRef.current.rotation.x = 0;
        meshRef.current.position.y = 0;
      }
    }
  });

  return (
    <group ref={groupRef} position={homePosition}>
      <group ref={meshRef} onClick={onClick} onPointerOver={() => document.body.style.cursor = 'pointer'} onPointerOut={() => document.body.style.cursor = 'auto'}>
        {/* Head */}
        <RoundedBox args={[0.4, 0.4, 0.4]} position={[0, 1.2, 0]} radius={0.05} castShadow>
          <meshStandardMaterial color={session.status === SessionStatus.ERROR ? '#ffaaaa' : skinColor} />
        </RoundedBox>
        {/* Torso */}
        <RoundedBox args={[0.5, 0.6, 0.3]} position={[0, 0.7, 0]} radius={0.05} castShadow>
          <meshStandardMaterial color={shirtColor} />
        </RoundedBox>
        {/* Left Arm (Pivot) */}
        <group ref={armLRef} position={[-0.35, 0.9, 0]}>
          <RoundedBox args={[0.15, 0.5, 0.15]} position={[0, -0.25, 0]} radius={0.05} castShadow>
            <meshStandardMaterial color={shirtColor} />
          </RoundedBox>
        </group>
        {/* Right Arm (Pivot) */}
        <group ref={armRRef} position={[0.35, 0.9, 0]}>
          <RoundedBox args={[0.15, 0.5, 0.15]} position={[0, -0.25, 0]} radius={0.05} castShadow>
            <meshStandardMaterial color={shirtColor} />
          </RoundedBox>
        </group>
        {/* Left Leg (Pivot) */}
        <group ref={legLRef} position={[-0.15, 0.4, 0]}>
          <RoundedBox args={[0.2, 0.4, 0.2]} position={[0, -0.2, 0]} radius={0.05} castShadow>
            <meshStandardMaterial color={pantsColor} />
          </RoundedBox>
        </group>
        {/* Right Leg (Pivot) */}
        <group ref={legRRef} position={[0.15, 0.4, 0]}>
          <RoundedBox args={[0.2, 0.4, 0.2]} position={[0, -0.2, 0]} radius={0.05} castShadow>
            <meshStandardMaterial color={pantsColor} />
          </RoundedBox>
        </group>
      </group>

      {/* Follow Camera */}
      {isFollowed && (
        <PerspectiveCamera 
          makeDefault 
          position={[0, 2.5, -4]} 
          rotation={[-0.2, Math.PI, 0]} 
          fov={60} 
        />
      )}

      {/* HTML Overlay */}
      <Html position={[0, 1.8, 0]} center zIndexRange={[100, 0]}>
        <motion.div 
          initial={{ opacity: 0, y: 10, scale: 0.8 }}
          animate={{ opacity: 1, y: 0, scale: 1 }}
          transition={{ type: "spring", stiffness: 300, damping: 20 }}
          className="flex flex-col items-center pointer-events-none transition-transform transform hover:scale-110"
        >
          {/* Thought Bubble */}
          {(session.status === SessionStatus.RUNNING || session.status === SessionStatus.ERROR || session.status === SessionStatus.BLOCKED || (agent && agent.current_tasks > 0)) && (
            <motion.div 
              initial={{ scale: 0 }} 
              animate={{ scale: 1 }} 
              className="mb-2 bg-white text-black px-3 py-1.5 rounded-2xl text-xs shadow-md border border-gray-200 relative"
            >
              <span className="font-semibold">
                {session.status === SessionStatus.RUNNING ? thought :
                 session.status === SessionStatus.ERROR ? 'Error!' :
                 session.status === SessionStatus.BLOCKED ? 'Needs Input' :
                 (agent && agent.current_tasks > 0) ? `${agent.current_tasks} tasks` : 'Working'}
              </span>
              <div className="absolute -bottom-1.5 left-1/2 transform -translate-x-1/2 w-3 h-3 bg-white border-b border-r border-gray-200 rotate-45"></div>
            </motion.div>
          )}
          {/* Name Tag */}
          <div className="bg-bg-primary text-text-primary px-2 py-1 rounded text-xs font-semibold whitespace-nowrap border border-border-primary shadow-sm flex flex-col items-center pointer-events-auto">
            <span>{agent?.agent_name || session.name || 'Agent'}</span>
            {agent && <span className="text-[9px] text-text-tertiary mb-1">{agent.role_description}</span>}
            <button 
              onClick={(e) => { e.stopPropagation(); onFollowToggle(); }}
              className="bg-blue-500 hover:bg-blue-600 text-white px-2 py-0.5 rounded text-[10px] w-full mt-0.5 transition-colors"
            >
              {isFollowed ? 'Stop' : '🎥 Follow'}
            </button>
          </div>
        </motion.div>
      </Html>
    </group>
  );
};

export const OfficeView = ({ sessions = [], spaces = [], sessionSpaces = {}, onSelectSession, activeOfficeId, managerSessionId, onManagerCreated, workspacePath, onRefreshSessions }: OfficeViewProps) => {
  const [officeAgents, setOfficeAgents] = useState<OfficeAgent[]>([]);
  const [showHireModal, setShowHireModal] = useState(false);
  const [isChatOpen, setIsChatOpen] = useState(true);
  const [followedAgentId, setFollowedAgentId] = useState<string | null>(null);

  const { connected, steps, sendPrompt, interrupt } = useHarness(managerSessionId || null, workspacePath, true, onManagerCreated, activeOfficeId);

  useEffect(() => {
    async function loadData() {
      try {
        const agents = await invoke<OfficeAgent[]>('get_office_agents', { officeId: activeOfficeId });
        setOfficeAgents(agents);
      } catch (err) {
        console.error("Failed to load office data:", err);
      }
    }
    loadData();
    
    const interval = setInterval(loadData, 2000);
    return () => clearInterval(interval);
  }, [activeOfficeId]);

  const groupedData: Record<string, Record<string, typeof sessions>> = {};
  spaces.forEach(space => { groupedData[space.id] = {}; });
  groupedData['unassigned'] = {};

  const officeSessions = sessions.filter(session => {
    return session.id === managerSessionId || 
           officeAgents.some(a => a.session_id === session.id) ||
           !!sessionSpaces[session.id];
  });

  officeSessions.forEach(session => {
    const spaceId = sessionSpaces[session.id] || 'unassigned';
    const wsPath = session.workspace || 'No Workspace';
    if (!groupedData[spaceId]) groupedData[spaceId] = {};
    if (!groupedData[spaceId][wsPath]) groupedData[spaceId][wsPath] = [];
    groupedData[spaceId][wsPath].push(session);
  });

  const getSpaceName = (spaceId: string) => {
    if (spaceId === 'unassigned') return 'Unassigned Agents';
    return spaces.find(s => s.id === spaceId)?.name || 'Unknown Space';
  };

  const BREAKROOM_POS: [number, number, number] = [0, 0, -35];
  const MEETINGROOM_POS: [number, number, number] = [-30, 0, -35];

  return (
    <div className="w-full h-full flex overflow-hidden bg-bg-secondary relative">
      
      {/* Main 3D View */}
      <div className="flex-1 relative min-w-0">
        
        {/* Hire Agent Button */}
        <div className="absolute top-4 left-4 z-10 bg-bg-primary/95 backdrop-blur border border-border-primary rounded-lg p-4 shadow-lg min-w-[200px]">
          <h3 className="text-sm font-bold text-text-primary mb-3">
            Office Staff
          </h3>
          
          <button 
            className="w-full text-xs font-semibold py-2 rounded bg-blue-600 hover:bg-blue-700 text-white transition-colors cursor-pointer flex items-center justify-center gap-2 shadow-sm"
            onClick={() => setShowHireModal(true)}
          >
            <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><line x1="19" y1="8" x2="19" y2="14"/><line x1="22" y1="11" x2="16" y2="11"/></svg>
            Hire Agent
          </button>
        </div>

        {!isChatOpen && (
          <button 
            onClick={() => setIsChatOpen(true)}
            className="absolute top-4 right-4 z-10 bg-bg-primary border border-border-primary rounded-lg p-3 shadow-lg hover:bg-bg-secondary transition-colors"
            title="Open CEO Chat"
          >
            <div className="w-2 h-2 rounded-full bg-blue-500 mb-1"></div>
            <div className="w-4 h-0.5 bg-text-tertiary"></div>
          </button>
        )}

        {showHireModal && activeOfficeId && (
          <HireAgentModal 
            officeId={activeOfficeId}
            spaces={spaces}
            workspacePath={workspacePath || null}
            onClose={() => setShowHireModal(false)}
            onAgentHired={() => {
              invoke('get_office_agents', { officeId: activeOfficeId }).then((agents: any) => setOfficeAgents(agents));
              if (onRefreshSessions) onRefreshSessions();
            }}
          />
        )}
        
        {followedAgentId && (
          <div className="absolute top-4 left-1/2 -translate-x-1/2 z-20">
            <button 
              onClick={() => setFollowedAgentId(null)}
              className="bg-red-500 hover:bg-red-600 text-white px-4 py-2 rounded-full shadow-xl font-bold flex items-center gap-2 transition-transform transform hover:scale-105"
            >
              🎥 Exit Follow Mode
            </button>
          </div>
        )}

        <Canvas shadows>
          <OrthographicCamera makeDefault={!followedAgentId} position={[20, 30, 30]} zoom={30} near={-100} far={100} />
          <OrbitControls makeDefault={!followedAgentId} enabled={!followedAgentId} enableRotate={false} enableZoom={true} enablePan={true} target={[0, 0, -10]} />
          <ambientLight intensity={0.5} />
          <directionalLight position={[10, 20, 10]} intensity={1} castShadow shadow-mapSize={[1024, 1024]} />
          
          <Bounds fit clip observe margin={1.2}>
          {(() => {
          const worldDeskPositions: Record<string, [number, number, number]> = {};
          const worldDeskRotations: Record<string, [number, number, number]> = {};
          const podCenters: Record<string, [number, number, number][]> = {};
          
          const occupiedCoords = new Set<string>();
          const markBox = (minX: number, maxX: number, minZ: number, maxZ: number) => {
            for (let x = Math.round(minX); x <= Math.round(maxX); x++) {
              for (let z = Math.round(minZ); z <= Math.round(maxZ); z++) {
                occupiedCoords.add(`${x},${z}`);
              }
            }
          };

          // Mark meeting room walls as occupied
          const mx = MEETINGROOM_POS[0];
          const mz = MEETINGROOM_POS[2];
          markBox(mx - 7, mx + 7, mz - 5, mz - 4); // Back
          markBox(mx - 7, mx + 7, mz + 4, mz + 5); // Front
          markBox(mx - 7, mx - 6, mz - 5, mz + 5); // Left
          markBox(mx + 6, mx + 7, mz - 5, mz - 2); // Right wall top
          markBox(mx + 6, mx + 7, mz + 2, mz + 5); // Right wall bottom

          // Mark breakroom walls as occupied
          const bx = BREAKROOM_POS[0];
          const bz = BREAKROOM_POS[2];
          markBox(bx - 6, bx + 6, bz - 6, bz - 5); // Back
          markBox(bx - 6, bx - 5, bz - 6, bz + 6); // Left
          markBox(bx - 6, bx - 2, bz + 5, bz + 6); // Front left
          markBox(bx + 2, bx + 6, bz + 5, bz + 6); // Front right
          markBox(bx - 1, bx + 1, bz - 3, bz - 1); // Coffee machine footprint
          
          Object.entries(groupedData).forEach(([spaceId, workspaces], spaceIndex) => {
            const workspaceEntries = Object.entries(workspaces);
            if (workspaceEntries.length === 0) return;
            const spaceOffsetX = spaceIndex * 40 - 10;
            podCenters[spaceId] = [];

            workspaceEntries.forEach(([_, workspaceSessions], wsIndex) => {
              const wsCols = 2;
              const wsX = spaceOffsetX + (wsIndex % wsCols) * 16 - 8;
              const wsZ = Math.floor(wsIndex / wsCols) * 16 - 8;

              let agentOffset = 0;
              workspaceSessions.forEach((session) => {
                if (session.id === managerSessionId) {
                  const mX = spaceOffsetX + 20;
                  const mZ = -20;
                  worldDeskPositions[session.id] = [mX, 0, mZ];
                  worldDeskRotations[session.id] = [0, 0, 0];
                  markBox(mX - 4, mX + 4, mZ - 4, mZ + 4);
                } else {
                  // Organize in pods of 4
                  const podIndex = Math.floor(agentOffset / 4);
                  const seatInPod = agentOffset % 4;
                  
                  const podCols = 2;
                  const podX = wsX + (podIndex % podCols) * 12 - 6;
                  const podZ = wsZ + Math.floor(podIndex / podCols) * 12 - 6;
                  
                  if (seatInPod === 0) {
                    podCenters[spaceId].push([podX, 0, podZ]);
                    // mark plant
                    markBox(podX - 1, podX + 1, podZ - 1, podZ + 1);
                  }
                  
                  let sX = 0, sZ = 0, rotY = 0;
                  if (seatInPod === 0) { sX = -2; sZ = -2; rotY = 0; }
                  else if (seatInPod === 1) { sX = -2; sZ = 2; rotY = Math.PI; }
                  else if (seatInPod === 2) { sX = 2; sZ = -2; rotY = 0; }
                  else if (seatInPod === 3) { sX = 2; sZ = 2; rotY = Math.PI; }

                  worldDeskPositions[session.id] = [podX + sX, 0, podZ + sZ];
                  worldDeskRotations[session.id] = [0, rotY, 0];
                  
                  // Mark desk as occupied
                  markBox(podX + sX - 2, podX + sX + 2, podZ + sZ - 1, podZ + sZ + 1);
                  
                  agentOffset++;
                }
              });
            });
          });

          const isWalkable = (x: number, z: number) => !occupiedCoords.has(`${Math.round(x)},${Math.round(z)}`);

          return (
            <>
              <Breakroom position={BREAKROOM_POS} />
              <MeetingRoom position={MEETINGROOM_POS} />

              {Object.entries(groupedData).map(([spaceId, workspaces], spaceIndex) => {
                const workspaceEntries = Object.entries(workspaces);
                if (workspaceEntries.length === 0) return null;

                const spaceOffsetX = spaceIndex * 40 - 10;
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

                    {podCenters[spaceId]?.map((center, idx) => {
                      const localX = center[0] - spaceOffsetX;
                      return <PodPlant key={`plant-${idx}`} position={[localX, 0, center[2]]} />;
                    })}

                    {workspaceEntries.map(([wsPath, workspaceSessions], wsIndex) => {
                      const wsCols = 2;
                      const wsX = (wsIndex % wsCols) * 16 - 8;
                      const wsZ = Math.floor(wsIndex / wsCols) * 16 - 8;
                      const shortPath = wsPath.split('/').pop() || wsPath;

                      return (
                        <group key={wsPath} position={[wsX, 0, wsZ]}>
                          <Box args={[14, 0.1, 14]} position={[0, -0.45, 0]} receiveShadow>
                            <meshStandardMaterial color="#cbd5e1" />
                          </Box>
                          <Html position={[0, 0.1, -6.5]} center>
                            <div className="bg-bg-secondary/90 text-text-secondary px-2 py-1 rounded text-[10px] font-mono shadow-sm whitespace-nowrap border border-border-primary">
                              {shortPath}
                            </div>
                          </Html>

                          {workspaceSessions.map((session) => {
                            if (session.id === managerSessionId) return null;
                            const pos = worldDeskPositions[session.id] || [0, -0.2, 0];
                            const rot = worldDeskRotations[session.id] || [0, 0, 0];
                            const localX = pos[0] - (spaceOffsetX + wsX);
                            const localZ = pos[2] - wsZ;
                            return <Desk key={`desk-${session.id}`} position={[localX, -0.2, localZ]} rotation={rot} />;
                          })}
                        </group>
                      );
                    })}
                    
                    {Object.values(workspaces).flat().some(s => s.id === managerSessionId) && (
                      <ManagerDesk key={`mgr-${managerSessionId}`} position={[20, -0.2, -20]} />
                    )}
                  </group>
                );
              })}

              {sessions.map(session => {
                const agent = officeAgents.find(a => a.session_id === session.id);
                const deskPos = worldDeskPositions[session.id];
                if (!deskPos) return null;

                const homePos: [number, number, number] = [deskPos[0], 0.8, deskPos[2] - 1];
                let targetPos = [...homePos] as [number, number, number];
                
                const rot = worldDeskRotations[session.id];
                if (rot && rot[1] === Math.PI) {
                  homePos[2] = deskPos[2] + 1;
                  targetPos[2] = deskPos[2] + 1;
                }

                // Pathfinding Dummy Logic
                if (agent?.visiting_session_id) {
                  targetPos = [MEETINGROOM_POS[0], 0.8, MEETINGROOM_POS[2]];
                } 
                else if (agent && agent.current_tasks === 0 && session.status === SessionStatus.READY) {
                  const charCode = session.id.charCodeAt(session.id.length - 1);
                  const timeCycle = Math.floor(Date.now() / 15000);
                  if ((charCode + timeCycle) % 3 === 0) {
                    targetPos = [MEETINGROOM_POS[0] + (charCode % 5) - 2, 0.8, MEETINGROOM_POS[2] + (charCode % 2) - 1];
                  } else if ((charCode + timeCycle) % 3 === 1) {
                    targetPos = [BREAKROOM_POS[0] + (charCode % 3) - 1, 0.8, BREAKROOM_POS[2] + 2];
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
                      rotation={rot}
                      onClick={() => onSelectSession(session.id)}
                      isWalkable={isWalkable}
                      isFollowed={followedAgentId === session.id}
                      onFollowToggle={() => setFollowedAgentId(followedAgentId === session.id ? null : session.id)}
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

      {/* Side Chat Panel (CEO) */}
      <div 
        className={`bg-bg-primary border-l border-border-primary flex flex-col shrink-0 transition-[width] duration-300 ease-in-out overflow-hidden ${isChatOpen ? 'w-96' : 'w-0 border-l-0'}`}
      >
        <div className="w-96 flex flex-col h-full shrink-0">
          <div className="flex items-center justify-between p-4 border-b border-border-primary bg-bg-secondary shrink-0">
            <div className="font-bold text-sm text-text-primary flex items-center gap-2">
              <span className="w-2 h-2 rounded-full bg-blue-500 shadow-[0_0_8px_rgba(59,130,246,0.8)]"></span>
              CEO
            </div>
            <button onClick={() => setIsChatOpen(false)} className="text-text-tertiary hover:text-text-primary">
              <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="m13 17 5-5-5-5M6 17l5-5-5-5"/></svg>
            </button>
          </div>
          <div className="flex-1 min-h-0 relative">
            <ChatPanel 
              steps={steps}
              connected={connected}
              onSend={(p: string) => sendPrompt(p)}
              onInterrupt={interrupt}
              workspacePath={workspacePath}
            />
          </div>
        </div>
      </div>

    </div>
  );
};
