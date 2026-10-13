import { describe, it, expect } from 'vitest';
import { ledClass, timeAgo, isActive } from './ui';

describe('CRM UI Utilities', () => {
  describe('ledClass', () => {
    it('maps resolved/online/sent/ok to "ok"', () => {
      expect(ledClass('resolved')).toBe('ok');
      expect(ledClass('online')).toBe('ok');
      expect(ledClass('sent')).toBe('ok');
      expect(ledClass('ok')).toBe('ok');
    });

    it('maps pending to "warn"', () => {
      expect(ledClass('pending')).toBe('warn');
    });

    it('maps open/failed/offline/urgent to "crit"', () => {
      expect(ledClass('open')).toBe('crit');
      expect(ledClass('failed')).toBe('crit');
      expect(ledClass('offline')).toBe('crit');
      expect(ledClass('urgent')).toBe('crit');
    });

    it('maps unknown to "off"', () => {
      expect(ledClass('closed')).toBe('off');
      expect(ledClass('')).toBe('off');
    });
  });

  describe('timeAgo', () => {
    it('calculates human readable time deltas', () => {
      const now = Date.now();
      expect(timeAgo(new Date(now - 10 * 1000).toISOString())).toBe('just now');
      expect(timeAgo(new Date(now - 120 * 1000).toISOString())).toBe('2m ago');
      expect(timeAgo(new Date(now - 7200 * 1000).toISOString())).toBe('2h ago');
      expect(timeAgo(new Date(now - 86400 * 3 * 1000).toISOString())).toBe('3d ago');
    });
  });

  describe('isActive', () => {
    it('returns true if seen in last 7 days', () => {
      const threeDaysAgo = new Date(Date.now() - 3 * 24 * 3600 * 1000).toISOString();
      expect(isActive(threeDaysAgo)).toBe(true);
    });

    it('returns false if last seen more than 7 days ago', () => {
      const tenDaysAgo = new Date(Date.now() - 10 * 24 * 3600 * 1000).toISOString();
      expect(isActive(tenDaysAgo)).toBe(false);
    });
  });
});
