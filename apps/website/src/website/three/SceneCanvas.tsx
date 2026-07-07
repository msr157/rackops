import { useRef, useMemo } from 'react';
import { Canvas, useFrame } from '@react-three/fiber';
import { Float, Stars } from '@react-three/drei';
import * as THREE from 'three';
import { useReducedMotion } from '@/hooks/useReducedMotion';
import './SceneCanvas.css';

interface NodeData {
  position: [number, number, number];
  scale: [number, number, number];
  color: string;
  pulseOffset: number;
}

const NODES: NodeData[] = [
  { position: [-2.2, 0.4, 0], scale: [0.5, 1.4, 0.35], color: '#6366f1', pulseOffset: 0 },
  { position: [-0.8, -0.2, 0.6], scale: [0.45, 1.1, 0.3], color: '#818cf8', pulseOffset: 0.4 },
  { position: [0.8, 0.1, -0.4], scale: [0.55, 1.6, 0.38], color: '#14b8a6', pulseOffset: 0.8 },
  { position: [2.1, -0.3, 0.2], scale: [0.48, 1.25, 0.32], color: '#6366f1', pulseOffset: 1.2 },
  { position: [0, 0.8, 0.8], scale: [0.35, 0.7, 0.25], color: '#14b8a6', pulseOffset: 1.6 },
];

function ServerRack({ data }: { data: NodeData }) {
  const meshRef = useRef<THREE.Mesh>(null);

  useFrame(({ clock }) => {
    if (!meshRef.current) return;
    const mat = meshRef.current.material as THREE.MeshStandardMaterial;
    const pulse = (Math.sin(clock.elapsedTime * 2 + data.pulseOffset) + 1) * 0.5;
    mat.emissiveIntensity = 0.15 + pulse * 0.35;
  });

  return (
    <Float speed={1.2} rotationIntensity={0.15} floatIntensity={0.4}>
      <mesh ref={meshRef} position={data.position} scale={data.scale}>
        <boxGeometry args={[1, 1, 1]} />
        <meshStandardMaterial
          color="#111827"
          emissive={data.color}
          emissiveIntensity={0.2}
          metalness={0.85}
          roughness={0.25}
        />
      </mesh>
      <mesh position={[data.position[0], data.position[1] + data.scale[1] * 0.55, data.position[2]]} scale={[data.scale[0] * 0.9, 0.04, data.scale[2] * 0.9]}>
        <boxGeometry args={[1, 1, 1]} />
        <meshStandardMaterial color={data.color} emissive={data.color} emissiveIntensity={0.8} />
      </mesh>
    </Float>
  );
}

function MeshConnections() {
  const lines = useMemo(() => {
    const points: THREE.Vector3[] = [];
    for (let i = 0; i < NODES.length; i++) {
      for (let j = i + 1; j < NODES.length; j++) {
        if (Math.random() > 0.45) continue;
        const a = new THREE.Vector3(...NODES[i].position);
        const b = new THREE.Vector3(...NODES[j].position);
        const mid = a.clone().lerp(b, 0.5);
        mid.y += 0.6;
        const curve = new THREE.QuadraticBezierCurve3(a, mid, b);
        points.push(...curve.getPoints(24));
      }
    }
    return new THREE.BufferGeometry().setFromPoints(points);
  }, []);

  return (
    <line geometry={lines}>
      <lineBasicMaterial color="#14b8a6" transparent opacity={0.35} />
    </line>
  );
}

function ControlPlane() {
  return (
    <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, -1.1, 0]}>
      <ringGeometry args={[2.8, 3.2, 6]} />
      <meshStandardMaterial color="#6366f1" emissive="#6366f1" emissiveIntensity={0.15} transparent opacity={0.25} side={THREE.DoubleSide} />
    </mesh>
  );
}

function Scene({ animate }: { animate: boolean }) {
  const groupRef = useRef<THREE.Group>(null);

  useFrame(({ clock }) => {
    if (!groupRef.current || !animate) return;
    groupRef.current.rotation.y = Math.sin(clock.elapsedTime * 0.15) * 0.25;
  });

  return (
    <group ref={groupRef}>
      <ambientLight intensity={0.35} />
      <pointLight position={[4, 4, 4]} intensity={1.2} color="#818cf8" />
      <pointLight position={[-4, -2, 2]} intensity={0.6} color="#14b8a6" />
      <Stars radius={80} depth={40} count={1200} factor={2} saturation={0} fade speed={0.5} />
      <ControlPlane />
      <MeshConnections />
      {NODES.map((node, i) => (
        <ServerRack key={i} data={node} />
      ))}
    </group>
  );
}

interface SceneCanvasProps {
  className?: string;
}

export default function SceneCanvas({ className = '' }: SceneCanvasProps) {
  const reducedMotion = useReducedMotion();

  return (
    <div className={`scene-canvas ${className}`} aria-hidden="true">
      <Canvas
        camera={{ position: [0, 1.2, 6], fov: 42 }}
        dpr={reducedMotion ? 1 : [1, 1.5]}
        gl={{ antialias: true, alpha: true }}
      >
        <fog attach="fog" args={['#05070d', 8, 18]} />
        <Scene animate={!reducedMotion} />
      </Canvas>
    </div>
  );
}
