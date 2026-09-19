import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { api } from '../../lib/api';
import i18n from '../../lib/i18n';
import { Graph } from './Graph';

vi.mock('../../lib/api', () => ({ api: vi.fn() }));

const snapshot = {
  revision: 3,
  terms: [
    { id: 'term-1', name: 'Eventual consistency', description: 'Replicas converge over time.', tags: ['Architecture'] },
    { id: 'term-2', name: 'Availability', description: 'Requests continue to be served.', tags: ['Reliability'] },
  ],
  thoughts: [{ id: 'thought-1', thesis: 'Pick guarantees per task', body: 'Different actions need different guarantees.', tags: ['Architecture'] }],
  links: [
    { id: 'link-1', term_id: 'term-1', thought_id: 'thought-1' },
    { id: 'link-2', term_id: 'term-2', thought_id: 'thought-1' },
  ],
};

function renderGraph() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}><Graph /></QueryClientProvider>);
}

describe('knowledge graph', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en');
    vi.mocked(api).mockResolvedValue(snapshot);
  });

  it('loads the prepared graph API and reveals thought nodes on demand', async () => {
    const user = userEvent.setup();
    renderGraph();

    expect(await screen.findByRole('button', { name: 'Term: Eventual consistency' })).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Thought: Pick guarantees per task' })).not.toBeInTheDocument();
    expect(api).toHaveBeenCalledWith('/api/v1/graph', expect.objectContaining({ signal: expect.any(AbortSignal) }));

    await user.click(screen.getByRole('button', { name: 'Reveal thoughts' }));
    expect(screen.getByRole('button', { name: 'Thought: Pick guarantees per task' })).toBeVisible();
  });

  it('filters the ordinary list by topic and opens a node detail', async () => {
    const user = userEvent.setup();
    renderGraph();

    await screen.findByRole('navigation', { name: 'Topics' });
    await user.click(screen.getByRole('button', { name: /Reliability/ }));
    expect(screen.getAllByText('Availability')).toHaveLength(2);
    expect(screen.queryByText('Eventual consistency')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Term: Availability' }));
    expect(screen.getByRole('heading', { name: 'Availability' })).toBeVisible();
    expect(screen.getByText('Requests continue to be served.')).toBeVisible();
  });
});
