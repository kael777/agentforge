import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { AppProviders } from './app/AppProviders'

describe('App', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders the generic starter and reports a healthy backend', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            status: 'ok',
            service: 'agentforge-api',
            version: 'dev',
            env: 'local',
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    )

    render(
      <AppProviders>
        <App />
      </AppProviders>,
    )

    expect(
      screen.getByRole('heading', {
        name: '一个可继续生长的 React 前端基线',
      }),
    ).toBeInTheDocument()
    expect(await screen.findByText(/后端已连接/)).toBeInTheDocument()
  })
})
