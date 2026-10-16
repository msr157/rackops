import { describe, it, expect, beforeEach } from 'vitest';
import {
  getToken,
  setToken,
  clearToken,
  getActiveOrg,
  setActiveOrg,
  api,
} from './api';

describe('API State & Token Helpers', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  describe('token management', () => {
    it('returns empty string when no token exists', () => {
      expect(getToken()).toBe('');
    });

    it('sets and retrieves token correctly', () => {
      setToken('jwt-session-token-123');
      expect(getToken()).toBe('jwt-session-token-123');
    });

    it('clears token cleanly', () => {
      setToken('jwt-session-token-123');
      clearToken();
      expect(getToken()).toBe('');
    });

    it('generates backup download URL with session token', () => {
      setToken('jwt-session-token-123');
      expect(api.backupDownloadUrl('svc-1', 'dump_1.sql.gz')).toBe('/api/services/svc-1/backups/dump_1.sql.gz/download?token=jwt-session-token-123');
    });
  });

  describe('active organization management', () => {
    it('returns empty string when no org is active', () => {
      expect(getActiveOrg()).toBe('');
    });

    it('sets active org and dispatches rackops-org-changed event', () => {
      let eventFired = false;
      const handler = () => {
        eventFired = true;
      };
      window.addEventListener('rackops-org-changed', handler);

      setActiveOrg('org-engineering-01');
      expect(getActiveOrg()).toBe('org-engineering-01');
      expect(eventFired).toBe(true);

      window.removeEventListener('rackops-org-changed', handler);
    });
  });
});
