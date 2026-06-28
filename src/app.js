/* ===========================
   RackOps — app.js
   =========================== */

'use strict';

// ── Navbar scroll effect ──────────────────────────────────
const navbar = document.getElementById('navbar');
window.addEventListener('scroll', () => {
  navbar.classList.toggle('scrolled', window.scrollY > 40);
}, { passive: true });

// ── Mobile hamburger ──────────────────────────────────────
const hamburger = document.getElementById('hamburger');
const navLinks  = document.getElementById('nav-links');

hamburger.addEventListener('click', () => {
  const isOpen = navLinks.classList.toggle('open');
  hamburger.setAttribute('aria-expanded', String(isOpen));
});

// Close mobile menu on link click
navLinks.querySelectorAll('a').forEach(link => {
  link.addEventListener('click', () => {
    navLinks.classList.remove('open');
    hamburger.setAttribute('aria-expanded', 'false');
  });
});

// ── Intersection Observer — feature cards ─────────────────
const featureCards = document.querySelectorAll('.feature-card');

const cardObserver = new IntersectionObserver((entries) => {
  entries.forEach(entry => {
    if (entry.isIntersecting) {
      const delay = parseInt(entry.target.dataset.delay || '0', 10);
      setTimeout(() => {
        entry.target.classList.add('visible');
      }, delay);
      cardObserver.unobserve(entry.target);
    }
  });
}, { threshold: 0.12 });

featureCards.forEach(card => cardObserver.observe(card));

// ── Terminal animation ────────────────────────────────────
function runTerminalAnimation() {
  const lines = ['t1', 't2', 't3', 't4', 't5', 't6'];
  lines.forEach((id, i) => {
    const el = document.getElementById(id);
    if (el) {
      setTimeout(() => {
        el.classList.add('show');
      }, 700 + i * 600);
    }
  });
}

const terminalSection = document.querySelector('.about');
let terminalTriggered = false;

const terminalObserver = new IntersectionObserver((entries) => {
  entries.forEach(entry => {
    if (entry.isIntersecting && !terminalTriggered) {
      terminalTriggered = true;
      runTerminalAnimation();
    }
  });
}, { threshold: 0.3 });

if (terminalSection) terminalObserver.observe(terminalSection);

// ── Pipeline step hover labels ────────────────────────────
document.querySelectorAll('.pipeline-step').forEach(step => {
  step.addEventListener('mouseenter', () => {
    step.classList.add('active');
  });
  step.addEventListener('mouseleave', () => {
    step.classList.remove('active');
  });
});

// ── Copy command button ───────────────────────────────────
function copyCommand() {
  const text = 'git clone https://github.com/your-org/rackops.git\ncd rackops && git push origin main';
  if (navigator.clipboard) {
    navigator.clipboard.writeText(text).then(() => {
      const btn = document.getElementById('copy-btn');
      btn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 13l4 4L19 7"/></svg>';
      btn.style.color = '#22c55e';
      setTimeout(() => {
        btn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"></path></svg>';
        btn.style.color = '';
      }, 2000);
    });
  }
}

// ── Smooth scroll for anchor links ───────────────────────
document.querySelectorAll('a[href^="#"]').forEach(anchor => {
  anchor.addEventListener('click', function (e) {
    const target = document.querySelector(this.getAttribute('href'));
    if (target) {
      e.preventDefault();
      target.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }
  });
});
