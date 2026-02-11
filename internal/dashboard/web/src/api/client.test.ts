import { describe, it, expect, vi, beforeEach } from 'vitest'

// We test URL construction by intercepting fetch calls.
// The client module uses global fetch, so we mock it.

const BASE = '/api'

// Helper: build URL the same way client.ts does for issue-state endpoints
function buildIssueUrl(path: string, clusterId?: string): string {
  return clusterId
    ? `${BASE}${path}?clusterId=${encodeURIComponent(clusterId)}`
    : `${BASE}${path}`
}

describe('issue-state API URL construction', () => {
  it('acknowledgeIssue URL includes clusterId when provided', () => {
    const url = buildIssueUrl('/issues/acknowledge', 'cluster-a')
    expect(url).toBe('/api/issues/acknowledge?clusterId=cluster-a')
  })

  it('acknowledgeIssue URL has no query param when clusterId omitted', () => {
    const url = buildIssueUrl('/issues/acknowledge')
    expect(url).toBe('/api/issues/acknowledge')
  })

  it('acknowledgeIssue URL has no query param when clusterId undefined', () => {
    const url = buildIssueUrl('/issues/acknowledge', undefined)
    expect(url).toBe('/api/issues/acknowledge')
  })

  it('snoozeIssue URL includes clusterId when provided', () => {
    const url = buildIssueUrl('/issues/snooze', 'prod-us-east')
    expect(url).toBe('/api/issues/snooze?clusterId=prod-us-east')
  })

  it('snoozeIssue URL has no query param when clusterId omitted', () => {
    const url = buildIssueUrl('/issues/snooze')
    expect(url).toBe('/api/issues/snooze')
  })

  it('unacknowledgeIssue URL includes clusterId', () => {
    const url = buildIssueUrl('/issues/acknowledge', 'cluster-b')
    expect(url).toBe('/api/issues/acknowledge?clusterId=cluster-b')
  })

  it('unsnoozeIssue URL includes clusterId', () => {
    const url = buildIssueUrl('/issues/snooze', 'cluster-b')
    expect(url).toBe('/api/issues/snooze?clusterId=cluster-b')
  })

  it('encodes special characters in clusterId', () => {
    const url = buildIssueUrl('/issues/acknowledge', 'cluster with spaces')
    expect(url).toBe('/api/issues/acknowledge?clusterId=cluster%20with%20spaces')
  })
})

describe('issue-state API calls via fetch mock', () => {
  let fetchSpy: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({}),
    })
    vi.stubGlobal('fetch', fetchSpy)
    vi.stubGlobal('AbortController', class {
      signal = {}
      abort() {}
    })
  })

  it('acknowledgeIssue sends clusterId as query param', async () => {
    const { acknowledgeIssue } = await import('./client')
    await acknowledgeIssue('ns/deploy/CrashLoop', undefined, 'cluster-x')

    expect(fetchSpy).toHaveBeenCalledOnce()
    const [url] = fetchSpy.mock.calls[0]
    expect(url).toContain('clusterId=cluster-x')
    expect(url).toContain('/api/issues/acknowledge')
  })

  it('acknowledgeIssue omits clusterId when not provided', async () => {
    // Re-import with fresh module for clean fetch spy state
    vi.resetModules()
    fetchSpy.mockResolvedValue({ ok: true, json: () => Promise.resolve({}) })
    const mod = await import('./client')
    await mod.acknowledgeIssue('ns/deploy/CrashLoop')

    expect(fetchSpy).toHaveBeenCalledOnce()
    const [url] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/issues/acknowledge')
  })

  it('snoozeIssue sends clusterId as query param', async () => {
    vi.resetModules()
    fetchSpy.mockResolvedValue({ ok: true, json: () => Promise.resolve({}) })
    const mod = await import('./client')
    await mod.snoozeIssue('ns/deploy/OOM', '1h', undefined, 'prod-cluster')

    expect(fetchSpy).toHaveBeenCalledOnce()
    const [url] = fetchSpy.mock.calls[0]
    expect(url).toContain('clusterId=prod-cluster')
    expect(url).toContain('/api/issues/snooze')
  })

  it('unsnoozeIssue sends clusterId as query param', async () => {
    vi.resetModules()
    fetchSpy.mockResolvedValue({ ok: true, json: () => Promise.resolve({}) })
    const mod = await import('./client')
    await mod.unsnoozeIssue('ns/deploy/OOM', 'staging')

    expect(fetchSpy).toHaveBeenCalledOnce()
    const [url] = fetchSpy.mock.calls[0]
    expect(url).toContain('clusterId=staging')
    expect(url).toContain('/api/issues/snooze')
  })
})
