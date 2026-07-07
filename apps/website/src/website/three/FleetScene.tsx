import { useMemo, useRef } from 'react';
import { Canvas, useFrame } from '@react-three/fiber';
import { Float } from '@react-three/drei';
import * as THREE from 'three';
import { useReducedMotion } from '@/hooks/useReducedMotion';
import './FleetScene.css';

const FLEET_NODES = [
  { id: 'mainframe', label: 'mayank-mainframe', position: [-2.5, 0, 0] as [number, number, number], color: '#6366f1' },
  { id: 'betterroad', label: 'BetterRoad-VM', position: [0, 0.3, 1.2] as [number, number, number], color: '#14b8a6' },
  { id: 'rackops', label: 'rackops-vm', position: [2.5, -0.2, 0] as [number, number, number], color: '#818cf8' },
];

function FleetNode({ position, color }: { position: [number, number, number]; color: string }) {
  const ref = useRef<THREE.Group>(null);

  useFrame(({ clock }) => {
    if (!ref.current) return;
    const pulse = (Math.sin(clock.elapsedTime * 1.5) + 1) * 0.5;
    ref.current.children.forEach((child, i) => {
      if (child instanceof THREE.Mesh && i > 0) {
        const mat = child.material as THREE.MeshStandardMaterial;
        mat.emissiveIntensity = 0.3 + pulse * 0.5;
      }
    });
  });

  return (
    <Float speed={0.8} floatIntensity={0.2}>
      <group ref={ref} position={position}>
        <mesh scale={[0.7, 1.8, 0.5]}>
          <boxGeometry args={[1, 1, 1]} />
          <meshStandardMaterial color="#0d1117" metalness={0.9} roughness={0.2} />
        </mesh>
        {[0.3, 0, -0.3].map((y, i) => (
          <mesh key={i} position={[0, y, 0.28]} scale={[0.55, 0.08, 0.02]}>
            <boxGeometry args={[1, 1, 1]} />
            <meshStandardMaterial color={color} emissive={color} emissiveIntensity={0.5} />
          </mesh>
        ))}
      </group>
    </Float>
  );
}

function FleetConnections() {
  const lineObj = useMemo(() => {
    const geom = new THREE.BufferGeometry().setFromPoints([
      new THREE.Vector3(...FLEET_NODES[0].position),
      new THREE.Vector3(...FLEET_NODES[1].position),
      new THREE.Vector3(...FLEET_NODES[1].position),
      new THREE.Vector3(...FLEET_NODES[2].position),
      new THREE.Vector3(...FLEET_NODES[0].position),
      new THREE.Vector3(...FLEET_NODES[2].position),
    ]);
    const mat = new THREE.LineBasicMaterial({ color: '#14b8a6', transparent: true, opacity: 0.5 });
    return new THREE.Line(geom, mat);
  }, []);

  return <primitive object={lineObj} />;
}

function FleetSceneInner() {
  const groupRef = useRef<THREE.Group>(null);

  useFrame(({ clock }) => {
    if (!groupRef.current) return;
    groupRef.current.rotation.y = clock.elapsedTime * 0.08;
  });

  return (
    <group ref={groupRef}>
      <ambientLight intensity={0.4} />
      <pointLight position={[3, 5, 3]} intensity={1} color="#818cf8" />
      <pointLight position={[-3, 2, -2]} intensity={0.5} color="#14b8a6" />
      <FleetConnections />
      {FLEET_NODES.map((node) => (
        <FleetNode key={node.id} position={node.position} color={node.color} />
      ))}
      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, -1.2, 0]}>
        <circleGeometry args={[4, 32]} />
        <meshStandardMaterial color="#6366f1" transparent opacity={0.06} side={THREE.DoubleSide} />
      </mesh>
    </group>
  );
}

export default function FleetScene() {
  const reducedMotion = useReducedMotion();

  return (
    <div className="fleet-scene" aria-hidden="true">
      <Canvas
        camera={{ position: [0, 2, 7], fov: 40 }}
        dpr={reducedMotion ? 1 : [1, 1.5]}
        gl={{ antialias: true, alpha: true }}
      >
        <fog attach="fog" args={['#0d1117', 6, 16]} />
        <FleetSceneInner />
      </Canvas>
      <div className="fleet-labels">
        {FLEET_NODES.map((node) => (
          <span key={node.id} className="fleet-label">{node.label}</span>
        ))}
      </div>
    </div>
  );
}
