import './Footer.css';

export default function Footer() {
  return (
    <footer className="footer">
      <div className="container footer-inner">
        <div className="footer-brand">
          <a href="#hero" className="logo" aria-label="RackOps Home">
            rackops
          </a>
          <p>
            The Infrastructure Operating System.
            <br />
            Your hardware. One control plane.
          </p>
        </div>
        <div className="footer-links">
          <div className="footer-col">
            <h4 className="mono-label">Product</h4>
            <ul>
              <li><a href="#platform">Platform</a></li>
              <li><a href="#ai">Intelligence</a></li>
              <li><a href="#install-lite">Install RackOps Lite</a></li>
            </ul>
          </div>
          <div className="footer-col">
            <h4 className="mono-label">Access</h4>
            <ul>
              <li><a href="https://github.com/msr157/rackops/releases/latest">Latest release</a></li>
              <li><a href="#access">Enterprise early access</a></li>
              <li><a href="https://github.com/msr157/rackops">GitHub source</a></li>
            </ul>
          </div>
        </div>
      </div>
      <div className="footer-bottom">
        <div className="container">
          <p className="mono-label">© 2026 RACKOPS / MIT LICENSE</p>
          <p className="mono-label">Made with care from India</p>
          <p className="mono-label footer-eof">[EOF]</p>
        </div>
      </div>
    </footer>
  );
}
