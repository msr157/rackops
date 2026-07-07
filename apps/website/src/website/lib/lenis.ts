import Lenis from 'lenis';
import { ScrollTrigger } from './gsap';

let lenisInstance: Lenis | null = null;

export function initLenis(): Lenis | null {
  if (typeof window === 'undefined') return null;
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return null;

  const lenis = new Lenis({
    duration: 1.2,
    easing: (t: number) => Math.min(1, 1.001 - Math.pow(2, -10 * t)),
    smoothWheel: true,
  });

  lenis.on('scroll', ScrollTrigger.update);

  const raf = (time: number) => {
    lenis.raf(time);
    requestAnimationFrame(raf);
  };
  requestAnimationFrame(raf);

  lenisInstance = lenis;
  return lenis;
}

export function destroyLenis(): void {
  lenisInstance?.destroy();
  lenisInstance = null;
}

export function getLenis(): Lenis | null {
  return lenisInstance;
}
