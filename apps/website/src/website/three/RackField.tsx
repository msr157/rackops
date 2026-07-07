import { useEffect, useMemo, useRef } from 'react';
import { Canvas, useFrame, useThree } from '@react-three/fiber';
import * as THREE from 'three';
import { useReducedMotion } from '@/hooks/useReducedMotion';
import './RackField.css';

// A dark field of instanced 1U servers with pulsing status LEDs — the hero
// backdrop. One WebGL canvas per page; keep this the only one.

const RACK_COLS = 3;
const RACK_ROWS = 2;
const UNITS_PER_RACK = 15;
const COUNT = RACK_COLS * RACK_ROWS * UNITS_PER_RACK;

const UNIT_W = 1.6;
const UNIT_H = 0.12;
const UNIT_D = 0.9;
const UNIT_GAP = 0.05;
const RACK_GAP_X = 2.6;
const RACK_GAP_Z = 2.4;

const SIGNAL = new THREE.Color('#3df582');
const SIGNAL_DIM = new THREE.Color('#1f7a44');
const AMBER = new THREE.Color('#ffb224');

interface UnitSpec {
  position: THREE.Vector3;
  phase: number;
  speed: number;
  amber: boolean;
}

function buildUnits(): UnitSpec[] {
  const units: UnitSpec[] = [];
  let i = 0;
  for (let rz = 0; rz < RACK_ROWS; rz++) {
    for (let rx = 0; rx < RACK_COLS; rx++) {
      for (let u = 0; u < UNITS_PER_RACK; u++) {
        const x = (rx - (RACK_COLS - 1) / 2) * RACK_GAP_X;
        const y = u * (UNIT_H + UNIT_GAP);
        const z = -rz * RACK_GAP_Z;
        units.push({
          position: new THREE.Vector3(x, y, z),
          // deterministic pseudo-random phase/speed per unit
          phase: (i * 2.399) % (Math.PI * 2),
          speed: i % 12 === 0 ? 6 : 1.2 + ((i * 7) % 10) / 10,
          amber: i === 23 || i === 61,
        });
        i++;
      }
    }
  }
  return units;
}

function Racks({ animate }: { animate: boolean }) {
  const ledsRef = useRef<THREE.InstancedMesh>(null);
  const groupRef = useRef<THREE.Group>(null);
  const pointer = useRef({ x: 0, y: 0 });
  const { invalidate } = useThree();

  const units = useMemo(buildUnits, []);

  const bodyMatrices = useMemo(() => {
    const m = new THREE.Matrix4();
    return units.map((u) => m.clone().setPosition(u.position));
  }, [units]);

  useEffect(() => {
    if (!animate) return;
    const onMove = (e: PointerEvent) => {
      pointer.current.x = (e.clientX / window.innerWidth) * 2 - 1;
      pointer.current.y = (e.clientY / window.innerHeight) * 2 - 1;
    };
    window.addEventListener('pointermove', onMove, { passive: true });
    return () => window.removeEventListener('pointermove', onMove);
  }, [animate]);

  // paint one static frame when motion is reduced
  useEffect(() => {
    const leds = ledsRef.current;
    if (!leds) return;
    const color = new THREE.Color();
    units.forEach((u, i) => {
      color.copy(u.amber ? AMBER : SIGNAL).lerp(SIGNAL_DIM, animate ? 0.5 : 0.4);
      leds.setColorAt(i, color);
    });
    if (leds.instanceColor) leds.instanceColor.needsUpdate = true;
    invalidate();
  }, [units, animate, invalidate]);

  useFrame(({ clock }) => {
    if (!animate) return;
    const t = clock.elapsedTime;

    const leds = ledsRef.current;
    if (leds) {
      const color = new THREE.Color();
      units.forEach((u, i) => {
        const pulse = (Math.sin(t * u.speed + u.phase) + 1) * 0.5;
        if (u.amber) {
          color.copy(AMBER).multiplyScalar(0.5 + pulse * 0.5);
        } else {
          color.copy(SIGNAL_DIM).lerp(SIGNAL, pulse);
        }
        leds.setColorAt(i, color);
      });
      if (leds.instanceColor) leds.instanceColor.needsUpdate = true;
    }

    const group = groupRef.current;
    if (group) {
      group.rotation.y = THREE.MathUtils.lerp(
        group.rotation.y,
        pointer.current.x * 0.06 + Math.sin(t * 0.1) * 0.02,
        0.05,
      );
      group.rotation.x = THREE.MathUtils.lerp(group.rotation.x, -pointer.current.y * 0.03, 0.05);
    }
  });

  const setBodyInstances = (mesh: THREE.InstancedMesh | null) => {
    if (!mesh) return;
    bodyMatrices.forEach((m, i) => mesh.setMatrixAt(i, m));
    mesh.instanceMatrix.needsUpdate = true;
  };

  const setLedInstances = (mesh: THREE.InstancedMesh | null) => {
    ledsRef.current = mesh;
    if (!mesh) return;
    const m = new THREE.Matrix4();
    units.forEach((u, i) => {
      m.setPosition(u.position.x + UNIT_W * 0.42, u.position.y, u.position.z + UNIT_D / 2 + 0.001);
      mesh.setMatrixAt(i, m);
    });
    mesh.instanceMatrix.needsUpdate = true;
  };

  return (
    <group ref={groupRef}>
      <instancedMesh ref={setBodyInstances} args={[undefined, undefined, COUNT]}>
        <boxGeometry args={[UNIT_W, UNIT_H, UNIT_D]} />
        <meshStandardMaterial color="#0e1210" metalness={0.6} roughness={0.45} />
      </instancedMesh>
      <instancedMesh ref={setLedInstances} args={[undefined, undefined, COUNT]}>
        <planeGeometry args={[0.06, 0.03]} />
        <meshBasicMaterial toneMapped={false} />
      </instancedMesh>
      <gridHelper args={[40, 40, '#1f7a44', '#0e1210']} position={[0, -0.1, 0]} />
      <ambientLight intensity={0.15} />
      <directionalLight position={[3, 6, 4]} intensity={0.5} color="#e9eee9" />
      <pointLight position={[-4, 1, 3]} intensity={4} color="#3df582" distance={12} />
    </group>
  );
}

export default function RackField({ className = '' }: { className?: string }) {
  const reducedMotion = useReducedMotion();

  return (
    <div className={`rack-field ${className}`} aria-hidden="true">
      <Canvas
        camera={{ position: [0, 1.6, 7], fov: 38 }}
        dpr={reducedMotion ? 1 : [1, 1.5]}
        frameloop={reducedMotion ? 'demand' : 'always'}
        gl={{ antialias: true, alpha: true }}
      >
        <fog attach="fog" args={['#050706', 6, 22]} />
        <Racks animate={!reducedMotion} />
      </Canvas>
    </div>
  );
}
