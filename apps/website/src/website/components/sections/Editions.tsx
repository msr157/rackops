import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { useScrollReveal } from '@/hooks/useScrollReveal';
import SectionRule from '@/components/SectionRule';
import './Editions.css';

const INSTALL_COMMAND =
  'curl -fsSL https://github.com/msr157/rackops/releases/latest/download/install.sh | sudo bash';

type Command = {
  label: string;
  value: string;
};

type InstallStep = {
  title: string;
  body: string;
  commands?: Command[];
};

type InstallOption = {
  id: 'linux' | 'macos' | 'wsl2' | 'source';
  label: string;
  summary: string;
  steps: InstallStep[];
};

const INSTALL_OPTIONS: InstallOption[] = [
  {
    id: 'linux',
    label: 'Linux',
    summary: 'For x86-64 and ARM machines running a modern Linux distribution.',
    steps: [
      {
        title: 'Use an account with sudo access',
        body: 'The installer adds the RackOps binary, Docker, Compose, Git, and a system service.',
      },
      {
        title: 'Run the one-line installer',
        body: 'Re-running this command later updates RackOps in place.',
        commands: [{ label: 'Terminal', value: INSTALL_COMMAND }],
      },
      {
        title: 'Open the control plane',
        body: 'Visit http://localhost:8080 when the installer finishes.',
      },
    ],
  },
  {
    id: 'macos',
    label: 'macOS',
    summary: 'For Apple Silicon and Intel Macs, macOS 11 or newer.',
    steps: [
      {
        title: 'Have a container runtime ready',
        body: 'Docker Desktop or colima — the installer detects either. RackOps starts without one, but app deploys need it.',
        commands: [
          { label: 'Homebrew (optional)', value: 'brew install colima docker && colima start' },
        ],
      },
      {
        title: 'Run the one-line installer',
        body: 'Installs the binary and a launchd service that starts on boot. Re-running this command later updates RackOps in place.',
        commands: [{ label: 'Terminal', value: INSTALL_COMMAND }],
      },
      {
        title: 'Open the control plane',
        body: 'Visit http://localhost:8080 when the installer finishes.',
      },
    ],
  },
  {
    id: 'wsl2',
    label: 'Windows / WSL2',
    summary: 'Run RackOps on Windows through Ubuntu on WSL2.',
    steps: [
      {
        title: 'Enable WSL2',
        body: 'Open PowerShell as Administrator, install WSL2, then restart Windows.',
        commands: [{ label: 'PowerShell', value: 'wsl --install' }],
      },
      {
        title: 'Install RackOps inside Ubuntu',
        body: 'Open Ubuntu from the Start menu and run the installer there.',
        commands: [{ label: 'Ubuntu', value: INSTALL_COMMAND }],
      },
      {
        title: 'Open the dashboard from Windows',
        body: 'Visit http://localhost:8080 in your regular Windows browser.',
      },
    ],
  },
  {
    id: 'source',
    label: 'Build from source',
    summary: 'For contributors who want to build the dashboard and Go binary locally.',
    steps: [
      {
        title: 'Prepare the toolchain',
        body: 'Install Node.js, pnpm 9, Go 1.26, and a running Docker engine.',
      },
      {
        title: 'Clone, build, and run',
        body: 'The dashboard is embedded into the final Go executable.',
        commands: [
          {
            label: 'Terminal',
            value: `git clone https://github.com/msr157/rackops.git
cd rackops
corepack enable
pnpm install
pnpm --filter @rackops/dashboard-lite build
cd apps/rackops-lite/backend
go build -o rackops ./cmd/rackops
./rackops server`,
          },
        ],
      },
      {
        title: 'Open the local dashboard',
        body: 'Visit http://localhost:8080. The binary manages its own machine by default.',
      },
    ],
  },
];

function CommandBlock({ command, id }: { command: Command; id: string }) {
  const [status, setStatus] = useState<'idle' | 'copied' | 'error'>('idle');
  const resetTimer = useRef<number | null>(null);

  useEffect(() => {
    return () => {
      if (resetTimer.current !== null) window.clearTimeout(resetTimer.current);
    };
  }, []);

  const copy = async () => {
    if (resetTimer.current !== null) window.clearTimeout(resetTimer.current);

    try {
      await navigator.clipboard.writeText(command.value);
      setStatus('copied');
    } catch {
      setStatus('error');
    }

    resetTimer.current = window.setTimeout(() => setStatus('idle'), 2200);
  };

  const buttonLabel =
    status === 'copied' ? 'Copied' : status === 'error' ? 'Copy failed' : 'Copy command';

  return (
    <div className="install-command">
      <div className="install-command-bar">
        <span className="mono-label">{command.label}</span>
        <button
          type="button"
          className="install-copy mono-label"
          onClick={copy}
          aria-describedby={`${id}-command`}
        >
          {buttonLabel}
        </button>
      </div>
      <pre id={`${id}-command`} tabIndex={0}>
        <code>{command.value}</code>
      </pre>
    </div>
  );
}

export default function Editions() {
  const sectionRef = useScrollReveal<HTMLElement>(0.08);
  const tabsRef = useRef<HTMLDivElement>(null);
  const [activeOption, setActiveOption] = useState<InstallOption['id']>('linux');
  const option = INSTALL_OPTIONS.find((item) => item.id === activeOption) ?? INSTALL_OPTIONS[0];

  const handleTabKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;

    event.preventDefault();
    const currentIndex = INSTALL_OPTIONS.findIndex((item) => item.id === activeOption);
    let nextIndex = currentIndex;

    if (event.key === 'ArrowRight') nextIndex = (currentIndex + 1) % INSTALL_OPTIONS.length;
    if (event.key === 'ArrowLeft') {
      nextIndex = (currentIndex - 1 + INSTALL_OPTIONS.length) % INSTALL_OPTIONS.length;
    }
    if (event.key === 'Home') nextIndex = 0;
    if (event.key === 'End') nextIndex = INSTALL_OPTIONS.length - 1;

    const nextOption = INSTALL_OPTIONS[nextIndex];
    setActiveOption(nextOption.id);
    window.requestAnimationFrame(() => {
      tabsRef.current?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[nextIndex]?.focus();
    });
  };

  return (
    <section className="section editions" id="editions" ref={sectionRef}>
      <div className="container">
        <SectionRule index="07" name="Editions" />
        <h2 className="reveal">RackOps Lite is live.</h2>
        <p className="section-body reveal">
          Install the self-hosted control plane on Linux, macOS, or Windows through WSL2.
          Pick a path and follow the guide.
        </p>

        <div className="editions-layout">
          <article className="lite-installer reveal" id="install-lite">
            <header className="lite-installer-header">
              <div>
                <p className="mono-label signal install-availability">Available now</p>
                <h3>Install RackOps Lite</h3>
                <p>
                  One binary, one dashboard, and no hosted control plane between you and
                  your machines.
                </p>
              </div>
              <a
                className="release-link mono-label"
                href="https://github.com/msr157/rackops/releases/latest"
                target="_blank"
                rel="noreferrer"
              >
                View latest release
              </a>
            </header>

            <div className="install-tabs" role="tablist" aria-label="Installation method" ref={tabsRef}>
              {INSTALL_OPTIONS.map((item) => (
                <button
                  key={item.id}
                  id={`install-tab-${item.id}`}
                  type="button"
                  role="tab"
                  aria-selected={item.id === activeOption}
                  aria-controls={`install-panel-${item.id}`}
                  tabIndex={item.id === activeOption ? 0 : -1}
                  onClick={() => setActiveOption(item.id)}
                  onKeyDown={handleTabKeyDown}
                >
                  {item.label}
                </button>
              ))}
            </div>

            <div
              className="install-panel"
              id={`install-panel-${option.id}`}
              role="tabpanel"
              aria-labelledby={`install-tab-${option.id}`}
              key={option.id}
            >
              <p className="install-summary">{option.summary}</p>
              <ol className="install-steps">
                {option.steps.map((step, stepIndex) => (
                  <li key={step.title}>
                    <h4>{step.title}</h4>
                    <p>{step.body}</p>
                    {step.commands?.map((command, commandIndex) => (
                      <CommandBlock
                        key={`${command.label}-${commandIndex}`}
                        command={command}
                        id={`${option.id}-${stepIndex}-${commandIndex}`}
                      />
                    ))}
                  </li>
                ))}
              </ol>
            </div>

            <dl className="install-facts">
              <div>
                <dt>Hosts</dt>
                <dd>Linux / macOS / WSL2</dd>
              </div>
              <div>
                <dt>Architectures</dt>
                <dd>x86-64 / ARM64</dd>
              </div>
              <div>
                <dt>Dashboard</dt>
                <dd>localhost:8080</dd>
              </div>
            </dl>
          </article>

          <aside className="enterprise-edition reveal">
            <p className="mono-label enterprise-status">In development</p>
            <h3>RackOps Enterprise</h3>
            <p className="enterprise-copy">
              Built for production fleets that need centralized identity, policy, and
              resilient control planes.
            </p>
            <ul className="enterprise-capabilities">
              <li>Multi-server orchestration</li>
              <li>RBAC, SSO, and audit logs</li>
              <li>High availability</li>
              <li>Private deployments</li>
            </ul>
            <a href="#access" className="btn btn-outline">
              Join enterprise early access
            </a>
          </aside>
        </div>
      </div>
    </section>
  );
}
