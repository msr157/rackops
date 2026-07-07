import './Footer.css';

export default function Footer() {
  return (
    <footer className="footer">
      <div className="container footer-inner">
        <div className="footer-brand">
          <a href="#hero" className="logo" aria-label="RackOps Home">
            <span className="logo-icon" aria-hidden="true">⬡</span>
            <span className="logo-text">RackOps</span>
          </a>
          <p>
            The Infrastructure Operating System.
            <br />
            Built for self-hosted and production teams.
          </p>
        </div>
        <div className="footer-links">
          <div className="footer-col">
            <h4>Product</h4>
            <ul>
              <li><a href="#features">Features</a></li>
              <li><a href="#architecture">Architecture</a></li>
              <li><a href="#solution">Solution</a></li>
            </ul>
          </div>
          <div className="footer-col">
            <h4>Editions</h4>
            <ul>
              <li><a href="#features">RackOps Lite</a></li>
              <li><a href="#features">RackOps Enterprise</a></li>
              <li><a href="#cta">Quick Start</a></li>
            </ul>
          </div>
          <div className="footer-col">
            <h4>Open Source</h4>
            <ul>
              <li><a href="https://github.com/your-org/rackops" target="_blank" rel="noopener noreferrer">GitHub</a></li>
              <li><a href="/LICENSE">License (MIT)</a></li>
              <li><a href="#cta">Documentation</a></li>
            </ul>
          </div>
        </div>
      </div>
      <div className="footer-bottom">
        <div className="container">
          <p>© 2026 RackOps. Released under the MIT License.</p>
          <p>Made with care from India</p>
        </div>
      </div>
    </footer>
  );
}
