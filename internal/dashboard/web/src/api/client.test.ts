import { describe, it, expect, vi, beforeEach } from 'vitest'

// ---------------------------------------------------------------------------
// Real API function tests — verify actual client functions send correct
// HTTP method, URL (with/without clusterId), headers, and body.
// ---------------------------------------------------------------------------

describe('acknowledgeIssue', () => {
  let fetchSpy: ReturnType<typeof vi.fn>

  beforeEach(() => {
    vi.resetModules()
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

  it('sends POST to /api/issues/acknowledge with key and reason in body', async () => {
    const { acknowledgeIssue } = await import('./client')
    await acknowledgeIssue('ns/deploy/CrashLoop', 'known issue')

    expect(fetchSpy).toHaveBeenCalledOnce()
    const [url, init] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/issues/acknowledge')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body)).toEqual({ key: 'ns/deploy/CrashLoop', reason: 'known issue' })
    expect(init.headers['Content-Type']).toBe('application/json')
  })

  it('appends clusterId as query param when provided', async () => {
    const { acknowledgeIssue } = await import('./client')
    await acknowledgeIssue('ns/deploy/CrashLoop', undefined, 'cluster-x')

    const [url] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/issues/acknowledge?clusterId=cluster-x')
  })

  it('omits clusterId query param when not provided', async () => {
    const { acknowledgeIssue } = await import('./client')
    await acknowledgeIssue('ns/deploy/CrashLoop')

    const [url] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/issues/acknowledge')
    expect(url).not.toContain('clusterId')
  })

  it('encodes special characters in clusterId', async () => {
    const { acknowledgeIssue } = await import('./client')
    await acknowledgeIssue('key', undefined, 'cluster with spaces')

    const [url] = fetchSpy.mock.calls[0]
    expect(url).toContain('clusterId=cluster%20with%20spaces')
  })
})

describe('unacknowledgeIssue', () => {
  let fetchSpy: ReturnType<typeof vi.fn>

  beforeEach(() => {
    vi.resetModules()
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

  it('sends DELETE to /api/issues/acknowledge with key in body', async () => {
    const { unacknowledgeIssue } = await import('./client')
    await unacknowledgeIssue('ns/deploy/CrashLoop')

    expect(fetchSpy).toHaveBeenCalledOnce()
    const [url, init] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/issues/acknowledge')
    expect(init.method).toBe('DELETE')
    expect(JSON.parse(init.body)).toEqual({ key: 'ns/deploy/CrashLoop' })
  })

  it('appends clusterId when provided', async () => {
    const { unacknowledgeIssue } = await import('./client')
    await unacknowledgeIssue('key', 'cluster-b')

    const [url] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/issues/acknowledge?clusterId=cluster-b')
  })
})

describe('snoozeIssue', () => {
  let fetchSpy: ReturnType<typeof vi.fn>

  beforeEach(() => {
    vi.resetModules()
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

  it('sends POST to /api/issues/snooze with key, duration, and reason', async () => {
    const { snoozeIssue } = await import('./client')
    await snoozeIssue('ns/deploy/OOM', '1h', 'investigating')

    expect(fetchSpy).toHaveBeenCalledOnce()
    const [url, init] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/issues/snooze')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body)).toEqual({ key: 'ns/deploy/OOM', duration: '1h', reason: 'investigating' })
  })

  it('appends clusterId when provided', async () => {
    const { snoozeIssue } = await import('./client')
    await snoozeIssue('ns/deploy/OOM', '1h', undefined, 'prod-cluster')

    const [url] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/issues/snooze?clusterId=prod-cluster')
  })

  it('omits clusterId when not provided', async () => {
    const { snoozeIssue } = await import('./client')
    await snoozeIssue('ns/deploy/OOM', '1h')

    const [url] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/issues/snooze')
    expect(url).not.toContain('clusterId')
  })
})

describe('unsnoozeIssue', () => {
  let fetchSpy: ReturnType<typeof vi.fn>

  beforeEach(() => {
    vi.resetModules()
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

  it('sends DELETE to /api/issues/snooze with key in body', async () => {
    const { unsnoozeIssue } = await import('./client')
    await unsnoozeIssue('ns/deploy/OOM')

    expect(fetchSpy).toHaveBeenCalledOnce()
    const [url, init] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/issues/snooze')
    expect(init.method).toBe('DELETE')
    expect(JSON.parse(init.body)).toEqual({ key: 'ns/deploy/OOM' })
  })

  it('appends clusterId when provided', async () => {
    const { unsnoozeIssue } = await import('./client')
    await unsnoozeIssue('ns/deploy/OOM', 'staging')

    const [url] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/issues/snooze?clusterId=staging')
  })
})

describe('response normalization', () => {
  let fetchSpy: ReturnType<typeof vi.fn>

  beforeEach(() => {
    vi.resetModules()
    fetchSpy = vi.fn()
    vi.stubGlobal('fetch', fetchSpy)
    vi.stubGlobal('AbortController', class {
      signal = {}
      abort() {}
    })
  })

  it('normalizes causal groups null to []', async () => {
    fetchSpy.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ groups: null, totalIssues: 0, uncorrelatedCount: 0 }),
    })

    const { fetchCausalGroups } = await import('./client')
    const data = await fetchCausalGroups('cluster-a')
    expect(data.groups).toEqual([])
  })

  it('normalizes fleet clusters null to []', async () => {
    fetchSpy.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ clusters: null }),
    })

    const { fetchFleetSummary } = await import('./client')
    const data = await fetchFleetSummary()
    expect(data.clusters).toEqual([])
  })
})
