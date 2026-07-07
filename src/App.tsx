import { useEffect } from 'react';
import { ScrollTrigger } from '@/lib/gsap';
import { initLenis, destroyLenis } from '@/lib/lenis';
import Navbar from '@/components/layout/Navbar';
import Footer from '@/components/layout/Footer';
import Hero from '@/components/sections/Hero';
import Problem from '@/components/sections/Problem';
import Solution from '@/components/sections/Solution';
import Architecture from '@/components/sections/Architecture';
import Features from '@/components/sections/Features';
import AI from '@/components/sections/AI';
import CTA from '@/components/sections/CTA';

export default function App() {
  useEffect(() => {
    initLenis();
    ScrollTrigger.refresh();

    return () => {
      destroyLenis();
      ScrollTrigger.getAll().forEach((t) => t.kill());
    };
  }, []);

  return (
    <>
      <Navbar />
      <main>
        <Hero />
        <Problem />
        <Solution />
        <Architecture />
        <Features />
        <AI />
        <CTA />
      </main>
      <Footer />
    </>
  );
}
