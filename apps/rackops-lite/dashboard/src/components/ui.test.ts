import { describe, it, expect } from 'vitest';
import {
  ledClass,
  dnsLabel,
  formatBytes,
  timeAgo,
  formatUptime,
  shortGpu,
  publicUrls,
  uid,
} from './ui';
import type { Service, IngressConfig } from '../api';

describe('UI Utilities', () => {
  describe('ledClass', () => {
    it('maps healthy statuses to "ok"', () => {
      expect(ledClass('running')).toBe('ok');
      expect(ledClass('online')).toBe('ok');
      expect(ledClass('verified')).toBe('ok');
      expect(ledClass('success')).toBe('ok');
      expect(ledClass('ok')).toBe('ok');
    });

    it('maps transitional statuses to "warn"', () => {
      expect(ledClass('building')).toBe('warn');
      expect(ledClass('pending')).toBe('warn');
      expect(ledClass('deploying')).toBe('warn');
      expect(ledClass('starting')).toBe('warn');
      expect(ledClass('queued')).toBe('warn');
    });

    it('maps failure statuses to "crit"', () => {
      expect(ledClass('failed')).toBe('crit');
      expect(ledClass('offline')).toBe('crit');
      expect(ledClass('misconfigured')).toBe('crit');
      expect(ledClass('error')).toBe('crit');
      expect(ledClass('critical')).toBe('crit');
    });

    it('maps stopped and unknown statuses to "off"', () => {
      expect(ledClass('stopped')).toBe('off');
      expect(ledClass('unknown')).toBe('off');
      expect(ledClass('')).toBe('off');
    });
  });

  describe('dnsLabel', () => {
    it('cleans names into valid DNS labels', () => {
      expect(dnsLabel('My Cool App')).toBe('my-cool-app');
      expect(dnsLabel('service_name.v1')).toBe('service-name-v1');
      expect(dnsLabel('---test---')).toBe('test');
      expect(dnsLabel('Special @#$ Characters!')).toBe('special-characters');
    });

    it('defaults to "app" when stripped clean', () => {
      expect(dnsLabel('')).toBe('app');
      expect(dnsLabel('---')).toBe('app');
      expect(dnsLabel('@#$')).toBe('app');
    });
  });

  describe('formatBytes', () => {
    it('formats binary sizes with human units', () => {
      expect(formatBytes(0)).toBe('0 B');
      expect(formatBytes(-100)).toBe('0 B');
      expect(formatBytes(512)).toBe('512 B');
      expect(formatBytes(1024)).toBe('1.0 KB');
      expect(formatBytes(1024 * 1024)).toBe('1.0 MB');
      expect(formatBytes(1.5 * 1024 * 1024 * 1024)).toBe('1.5 GB');
      expect(formatBytes(2 * 1024 * 1024 * 1024 * 1024)).toBe('2.0 TB');
    });
  });

  describe('timeAgo', () => {
    it('renders readable relative times', () => {
      const now = Date.now();
      expect(timeAgo(new Date(now - 10 * 1000).toISOString())).toBe('just now');
      expect(timeAgo(new Date(now - 120 * 1000).toISOString())).toBe('2m ago');
      expect(timeAgo(new Date(now - 7200 * 1000).toISOString())).toBe('2h ago');
      expect(timeAgo(new Date(now - 86400 * 2 * 1000).toISOString())).toBe('2d ago');
    });
  });

  describe('formatUptime', () => {
    it('formats seconds into compact duration strings', () => {
      expect(formatUptime(45)).toBe('0m');
      expect(formatUptime(300)).toBe('5m');
      expect(formatUptime(3665)).toBe('1h 1m');
      expect(formatUptime(90000)).toBe('1d 1h');
    });
  });

  describe('shortGpu', () => {
    it('strips verbose PCI vendor strings', () => {
      const verbose = 'Advanced Micro Devices, Inc. [AMD/ATI] Navi 21 [Radeon RX 6800/6800 XT / 6900 XT] (rev c1)';
      expect(shortGpu(verbose)).toBe('AMD Navi 21 [Radeon RX 6800/6800 XT / 6900 XT]');
    });
  });

  describe('publicUrls', () => {
    it('generates Front Door HTTPS URL', () => {
      const svc = {
        domain: 'app.link.rackops.in',
        frontDoor: true,
      } as Service;
      const urls = publicUrls(svc, null);
      expect(urls).toEqual([{ url: 'https://app.link.rackops.in', kind: 'frontdoor' }]);
    });

    it('generates Ingress HTTP / HTTPS URLs', () => {
      const svc = {
        domain: 'mytest.example.com',
      } as Service;
      const ingressTLS: IngressConfig = {
        enabled: true,
        nodeId: 'node-1',
        httpPort: 80,
        httpsPort: 443,
        acmeEmail: 'admin@example.com',
      };
      const urls = publicUrls(svc, ingressTLS);
      expect(urls).toEqual([{ url: 'https://mytest.example.com', kind: 'domain' }]);
    });

    it('includes QuickTunnel URL when present', () => {
      const svc = {
        quickTunnelUrl: 'https://cool-tunnel.trycloudflare.com',
      } as Service;
      const urls = publicUrls(svc, null);
      expect(urls).toEqual([{ url: 'https://cool-tunnel.trycloudflare.com', kind: 'tunnel' }]);
    });
  });

  describe('uid', () => {
    it('returns a non-empty string identifier', () => {
      const id1 = uid();
      const id2 = uid();
      expect(id1).toBeTruthy();
      expect(id2).toBeTruthy();
      expect(id1).not.toBe(id2);
    });
  });
});
