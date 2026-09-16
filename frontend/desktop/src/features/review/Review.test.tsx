import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { api, ApiError } from '../../lib/api';
import i18n from '../../lib/i18n';
import { conspect, tags } from '../../test/fixtures';
import type { Conspect, Decision } from './types';
import { Review } from './Review';
import { TagSelector } from './TagSelector';
import { TaxonomyEditor } from './TaxonomyEditor';
import { findTextMatches } from './SourceText';
vi.mock('../../lib/api', async importOriginal => { const actual = await importOriginal<typeof import('../../lib/api')>(); return { ...actual, api: vi.fn() }; });
beforeEach(async () => { vi.mocked(api).mockReset(); await i18n.changeLanguage('en'); });
function renderReview() { return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })}><Review /></QueryClientProvider>); }
function mockReview(fail = false) {
  const data = structuredClone(conspect); let applied = false;
  vi.mocked(api).mockImplementation(async (path, options) => {
    if (path.includes('/taxonomy/tag')) return tags as never;
    if (path.includes('/review/taxonomy/')) {
      const decision = JSON.parse(String(options?.body)) as { resolution: string; value?: string; target_id?: string };
      const item = data.taxonomy.find(item => path.endsWith(item.id))!;
      Object.assign(item, decision);
      data.status = data.items.every(item => item.status === 'resolved') && data.taxonomy.every(item => !item.required || item.resolution) ? 'ready_to_apply' : 'review_pending';
      return undefined as never;
    }
    if (path.includes('/review/items/')) {
      if (fail) { fail = false; throw new ApiError(503); }
      const decision = JSON.parse(String(options?.body)) as Decision;
      const item = data.items.find(item => path.endsWith(item.id))!; Object.assign(item, decision, { status: 'resolved' });
      data.status = data.items.every(item => item.status === 'resolved') && data.taxonomy.every(item => !item.required || item.resolution) ? 'ready_to_apply' : 'review_pending'; return undefined as never;
    }
    if (path.endsWith('/apply')) { applied = true; return undefined as never; }
    return (path.includes('note-1') ? structuredClone(data) : applied ? [] : [structuredClone(data)]) as never;
  });
  return data;
}
describe('Review flow', () => {
  it('flushes edits before moving, keeps Apply on the final screen, and removes applied notes', async () => {
    const user = userEvent.setup(); mockReview(); renderReview();
    const name = await screen.findByRole('textbox', { name: 'Name' });
    await user.clear(name); await user.type(name, 'My revised concept');
    expect(screen.queryByRole('button', { name: 'Apply conspect' })).not.toBeInTheDocument();
    await user.click(screen.getAllByRole('button', { name: 'Next' }).at(-1)!);
    expect(await screen.findByRole('textbox', { name: 'Thesis' })).toBeInTheDocument();
    expect(vi.mocked(api).mock.calls.some(([, options]) => String(options?.body).includes('My revised concept'))).toBe(true);
    await user.click(screen.getByRole('button', { name: 'Reject' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled());
    await user.click(screen.getByRole('button', { name: 'Save' }));
    await user.click(await screen.findByRole('button', { name: 'Apply conspect' }));
    expect(await screen.findByRole('heading', { name: 'All caught up' })).toBeInTheDocument();
  });
  it('stays on the edited item on save failure and supports retry', async () => {
    const user = userEvent.setup(); mockReview(true); renderReview();
    const name = await screen.findByRole('textbox', { name: 'Name' }); await user.type(name, ' revised');
    await user.click(screen.getAllByRole('button', { name: 'Next' }).at(-1)!);
    expect(await screen.findByText('Save failed')).toBeInTheDocument(); expect(name).toHaveValue('Eventual consistency revised'); expect(screen.queryByRole('textbox', { name: 'Thesis' })).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Try again' })); await screen.findByText('Saved');
    await user.keyboard('{Alt>}{ArrowRight}{/Alt}'); expect(await screen.findByRole('textbox', { name: 'Thesis' })).toBeInTheDocument();
  });
  it('shows database analogues separately and replaces pending matches with conspect relationships', async () => {
    mockReview(); renderReview(); await screen.findByRole('textbox', { name: 'Name' });
    expect(within(screen.getByRole('group', { name: 'Similar in the database' })).getAllByRole('button')).toHaveLength(1);
    expect(screen.getByText('Choose consistency for the user’s task')).toBeInTheDocument();
    expect(screen.queryByText('Not yet in the graph')).not.toBeInTheDocument();
  });
  it('forces an exact title conflict to keep or overwrite the existing entity', async () => {
    const user = userEvent.setup(); const data = mockReview();
    data.items[0].canonical_matches!.push({
      id: 'canonical-similar', kind: 'term', source: 'canonical', exact: false, version: 1,
      value: { name: 'Consistency model', description: 'A weaker suggestion that must be hidden.', tags: ['Architecture'] },
    });
    renderReview();
    const name = await screen.findByRole('textbox', { name: 'Name' });
    expect(within(screen.getByRole('group', { name: 'Similar in the database' })).getAllByRole('button')).toHaveLength(1);
    expect(screen.queryByText('Consistency model')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Create' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Reject' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Keep existing' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Overwrite' })).toBeInTheDocument();
    expect(screen.getByText(/entity with exactly this name already exists/i)).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Keep existing' }));
    expect(name).toHaveAttribute('readonly');
    expect(name).toHaveValue('Eventual consistency');
    await user.click(screen.getByRole('button', { name: 'Overwrite' }));
    expect(name).not.toHaveAttribute('readonly');
    expect(name).toHaveValue('Eventual consistency');
  });
  it('lets a non-exact analogue enter and leave focus', async () => {
    const user = userEvent.setup(), data = mockReview();
    data.items[0].canonical_matches![0].exact = false;
    renderReview(); await screen.findByRole('textbox', { name: 'Name' });
    const analogue = within(screen.getByRole('group', { name: 'Similar in the database' })).getByRole('button');
    expect(analogue).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByRole('button', { name: 'Create' })).toBeInTheDocument();
    await user.click(analogue);
    expect(analogue).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: 'Overwrite' })).toBeInTheDocument();
    await user.click(analogue);
    expect(analogue).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByRole('button', { name: 'Reject' })).toBeInTheDocument();
  });
  it('blocks accepting a thought while one of its terms is rejected', async () => {
    const user = userEvent.setup(), data = mockReview();
    data.items[0].canonical_matches![0].exact = false;
    renderReview(); await screen.findByRole('textbox', { name: 'Name' });
    await user.click(screen.getByRole('button', { name: 'Reject' }));
    await user.click(screen.getAllByRole('button', { name: 'Next' }).at(-1)!);
    await user.click(await screen.findByRole('button', { name: 'Create' }));
    await screen.findByText('Save failed');
    expect(screen.getAllByRole('alert').some(alert => /rejected term/i.test(alert.textContent || ''))).toBe(true);
    expect(vi.mocked(api).mock.calls.some(([path]) => path.endsWith('/review/items/item-2'))).toBe(false);
  });
  it('does not allow applying a still-preparing empty review', async () => {
    const data: Conspect = { ...structuredClone(conspect), status: 'preparing_review', items: [] };
    vi.mocked(api).mockImplementation(async path => (path.includes('/taxonomy/tag') ? tags : path.includes('note-1') ? data : [data]) as never);
    renderReview(); expect(await screen.findByText('Preparing your review…')).toBeInTheDocument(); expect(screen.queryByRole('button', { name: 'Apply conspect' })).not.toBeInTheDocument();
  });
  it('refreshes canonical matches after an Apply conflict and returns to the unresolved entity', async () => {
    const user = userEvent.setup(), data = mockReview();
    const implementation = vi.mocked(api).getMockImplementation()!;
    vi.mocked(api).mockImplementation(async (path, options) => {
      if (path.endsWith('/apply')) {
        data.items[0].status = 'pending'; data.items[0].resolution = ''; data.items[0].canonical_matches![0].version = 3; data.status = 'review_pending';
        throw new ApiError(409, 'review.conflict');
      }
      return implementation(path, options);
    });
    renderReview(); await screen.findByRole('textbox', { name: 'Name' });
    await user.click(screen.getByRole('button', { name: 'Overwrite' }));
    await user.click(screen.getAllByRole('button', { name: 'Next' }).at(-1)!);
    await user.click(await screen.findByRole('button', { name: 'Reject' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled());
    await user.click(screen.getByRole('button', { name: 'Save' })); await user.click(await screen.findByRole('button', { name: 'Apply conspect' }));
    expect(await screen.findByRole('textbox', { name: 'Name' })).toBeInTheDocument();
    expect(screen.getByText(/An entity changed in the graph/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Apply conspect' })).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Overwrite' })); await user.keyboard('{Alt>}{ArrowRight}{/Alt}');
    expect(vi.mocked(api).mock.calls.some(([, options]) => String(options?.body).includes('"target_version":3'))).toBe(true);
  });
  it('lets a thought add another term from the current conspect', async () => {
    const user = userEvent.setup(), data = mockReview();
    data.items.push({ id: 'item-3', kind: 'term', status: 'pending', resolution: '', target_version: 0, incoming_variants: [{ candidate_id: 'candidate-3', value: { name: 'Availability', description: 'The system continues serving requests.', tags: ['Architecture'] } }], canonical_matches: [], pending_matches: [], final_value: { name: 'Availability', description: 'The system continues serving requests.', tags: ['Architecture'] } });
    data.item_count = 3; data.unresolved_items = 3;
    renderReview();
    await user.click(await screen.findByRole('button', { name: 'Overwrite' }));
    await user.click(screen.getAllByRole('button', { name: 'Next' }).at(-1)!);
    const relationships = await screen.findByRole('group', { name: 'Entities in this thought' });
    const availability = within(relationships).getByRole('button', { name: /Availability/ });
    expect(availability).toHaveAttribute('aria-pressed', 'false');
    await user.click(availability);
    await user.click(screen.getAllByRole('button', { name: 'Next' }).at(-1)!);
    expect(vi.mocked(api).mock.calls.some(([, options]) => String(options?.body).includes('"related_item_ids":["item-1","item-3"]'))).toBe(true);
  });
  it('edits and resolves a new tag inside the entity card', async () => {
    const user = userEvent.setup(), data = mockReview();
    data.items[0].incoming_variants[0].value.tags = ['Resiliance'];
    data.items[0].final_value.tags = ['Resiliance'];
    data.taxonomy = [{ id: 'new-tag', kind: 'tag', value: 'Resiliance', resolution: '', required: true }];
    data.unresolved_tags = 1;
    renderReview();
    const input = await screen.findByRole('textbox', { name: 'Edit new tag Resiliance' });
    await user.clear(input); await user.type(input, 'Resilience');
    await user.click(screen.getAllByRole('button', { name: 'Next' }).at(-1)!);
    await waitFor(() => expect(vi.mocked(api).mock.calls.map(([path, options]) => [path, options?.body])).toContainEqual([
      '/api/v1/review/taxonomy/new-tag',
      JSON.stringify({ resolution: 'create', value: 'Resilience' }),
    ]));
  });
});
describe('controlled taxonomy', () => {
  it('filters only existing entries and cannot create text entered in search', async () => {
    const user = userEvent.setup(), change = vi.fn(); render(<TagSelector tags={tags} value={[]} onChange={change} />);
    await user.click(screen.getByRole('button', { name: 'Choose tags' })); await user.type(screen.getByRole('combobox'), 'invented tag{Enter}');
    expect(screen.getByText('No matching tags')).toBeInTheDocument(); expect(change).not.toHaveBeenCalled();
    await user.clear(screen.getByRole('combobox')); await user.keyboard('{ArrowDown}{Enter}'); expect(change).toHaveBeenCalledWith(['Distributed systems']);
  });
  it.each(['create', 'map', 'remove'] as const)('supports the %s decision for an LLM tag', async resolution => {
    const user = userEvent.setup(), save = vi.fn().mockResolvedValue(undefined); let flush: (() => Promise<boolean>) | undefined;
    render(<TaxonomyEditor item={{ id: 'new-1', kind: 'tag', value: 'Resiliance', resolution: '', required: true }} tags={tags} save={save} register={handle => { flush = handle?.flush; }} />);
    await user.click(screen.getByRole('button', { name: resolution === 'create' ? 'Create tag' : resolution === 'map' ? 'Map to existing' : 'Reject tag' }));
    if (resolution === 'create') { await user.clear(screen.getByRole('textbox', { name: 'Tag name' })); await user.type(screen.getByRole('textbox', { name: 'Tag name' }), 'Resilience'); }
    if (resolution === 'map') { await user.click(screen.getByRole('button', { name: 'Select an existing tag' })); await user.click(screen.getByRole('option', { name: 'Architecture' })); }
    await flush?.(); expect(save).toHaveBeenCalledWith(resolution === 'create' ? { resolution, value: 'Resilience' } : resolution === 'map' ? { resolution, target_id: 'tag-2' } : { resolution });
  });
});
it('searches source text literally and preserves character positions', () => {
  expect(findTextMatches('A [term]. Another [TERM].', '[term]')).toEqual([{ start: 2, end: 8 }, { start: 18, end: 24 }]); expect(findTextMatches('text', '')).toEqual([]);
});
