import { useEffect } from 'react';
import { ScrollTrigger } from '@/lib/gsap';
import { initLenis, destroyLenis } from '@/lib/lenis';
import Navbar from '@/components/layout/Navbar';
import Footer from '@/components/layout/Footer';
import Hero from '@/components/sections/Hero';
import Problem from '@/components/sections/Problem';
import Shift from '@/components/sections/Shift';
import Solution from '@/components/sections/Solution';
import Platform from '@/components/sections/Platform';
import Migration from '@/components/sections/Migration';
import AI from '@/components/sections/AI';
import Audience from '@/components/sections/Audience';
import Editions from '@/components/sections/Editions';
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
        <Shift />
        <Solution />
        <Platform />
        <Migration />
        <AI />
        <Audience />
        <Editions />
        <CTA />
      </main>
      <Footer />
    </>
  );
}
