import { describe, it, expect } from 'vitest';
import { cleanError } from './toast';

describe('cleanError', () => {
  it('strips Error: transport prefix', () => {
    expect(cleanError(new Error('something bad happened'))).toBe('something bad happened');
    expect(cleanError('Error: something bad happened')).toBe('something bad happened');
  });

  it('strips HTTP status code prefix from backend responses', () => {
    expect(cleanError('Error: 409: conflict with existing hostname')).toBe('conflict with existing hostname');
    expect(cleanError('Error: 404: node not found')).toBe('node not found');
    expect(cleanError('Error: 500: internal server failure')).toBe('internal server failure');
  });

  it('provides fallback for empty errors', () => {
    expect(cleanError('Error: ')).toBe('Something went wrong.');
    expect(cleanError('')).toBe('Something went wrong.');
  });
});
