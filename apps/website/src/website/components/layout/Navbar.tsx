import { useEffect, useState } from 'react';
import './Navbar.css';

const LINKS = [
  { href: '#problem', label: 'Problem' },
  { href: '#platform', label: 'Platform' },
  { href: '#ai', label: 'Intelligence' },
  { href: '#access', label: 'Enterprise' },
  { href: '#install-lite', label: 'Install Lite', cta: true },
];

export default function Navbar() {
  const [scrolled, setScrolled] = useState(false);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 40);
    window.addEventListener('scroll', onScroll, { passive: true });
    return () => window.removeEventListener('scroll', onScroll);
  }, []);

  const handleClick = (href: string) => {
    setOpen(false);
    const target = document.querySelector(href);
    if (target) target.scrollIntoView({ behavior: 'smooth', block: 'start' });
  };

  return (
    <nav className={`navbar ${scrolled ? 'scrolled' : ''}`} id="navbar">
      <div className="container nav-container">
        <a
          href="#hero"
          className="logo"
          aria-label="RackOps Home"
          onClick={(e) => {
            e.preventDefault();
            handleClick('#hero');
          }}
        >
          rackops
        </a>
        <ul className={`nav-links ${open ? 'open' : ''}`} id="nav-links">
          {LINKS.map((link) => (
            <li key={link.href}>
              <a
                href={link.href}
                className={link.cta ? 'btn-nav' : undefined}
                onClick={(e) => {
                  e.preventDefault();
                  handleClick(link.href);
                }}
              >
                {link.label}
              </a>
            </li>
          ))}
        </ul>
        <button
          className="hamburger"
          aria-label="Toggle menu"
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
        >
          <span />
          <span />
          <span />
        </button>
      </div>
    </nav>
  );
}
