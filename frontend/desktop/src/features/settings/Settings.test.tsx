import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import { api } from '../../lib/api';
import i18n from '../../lib/i18n';
import { settings } from '../../test/fixtures';
import * as bridge from '../../lib/bridge';
import { Settings } from './Settings';

vi.mock('../../lib/api', () => ({ api: vi.fn() }));
function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(<QueryClientProvider client={client}><Settings /></QueryClientProvider>);
}
beforeEach(async () => { vi.resetAllMocks(); await i18n.changeLanguage('en'); vi.mocked(api).mockResolvedValue(structuredClone(settings)); });
it('loads durations, rejects invalid input, cancels, and saves only the changed field', async () => {
  const user = userEvent.setup(); mount();
  await user.click(await screen.findByRole('tab', { name: 'For developers' }));
  const input = screen.getByRole('textbox', { name: 'Complete model request timeout' });
  expect(input).toHaveValue('10m');
  await user.clear(input); await user.type(input, '0s');
  expect(screen.getByRole('alert')).toHaveTextContent('positive duration');
  expect(screen.getByRole('button', { name: 'Save changes' })).toBeDisabled();
  await user.click(screen.getByRole('button', { name: 'Cancel' })); expect(input).toHaveValue('10m');
  await user.clear(input); await user.type(input, '45m');
  const updated = structuredClone(settings); updated.models.request_timeout = 2700000000000;
  vi.mocked(api).mockResolvedValueOnce({ settings: updated, restart_required: true, restart_keys: ['models.request_timeout'] });
  await user.click(screen.getByRole('button', { name: 'Save changes' }));
  await waitFor(() => expect(api).toHaveBeenLastCalledWith('/api/v1/settings', { method: 'PATCH', body: JSON.stringify({ model_request_timeout: '45m' }) }));
  expect(await screen.findByText('Restart the app to apply: Complete model request timeout.')).toBeVisible();
  expect(input).toHaveValue('45m');
});
it('keeps edits visible after a save failure', async () => {
  const user = userEvent.setup(); mount(); await user.click(await screen.findByRole('tab', { name: 'For developers' }));
  await user.click(screen.getByRole('button', { name: 'Speech recognition and libraries' }));
  const input = screen.getByRole('textbox', { name: 'Audio transcription timeout' });
  await user.clear(input); await user.type(input, '8m'); vi.mocked(api).mockRejectedValueOnce(new Error('failed'));
  await user.click(screen.getByRole('button', { name: 'Save changes' }));
  expect(await screen.findByRole('alert')).toBeVisible(); expect(input).toHaveValue('8m');
});
it('provides all translated labels and hints', async () => {
  await i18n.changeLanguage('ru'); mount();
  await screen.findByRole('tab', { name: i18n.t('developer') });
  await userEvent.setup().click(screen.getByRole('tab', { name: i18n.t('developer') }));
  await userEvent.setup().click(screen.getByRole('button', { name: i18n.t('audioSection') }));
  await userEvent.setup().click(screen.getByRole('button', { name: i18n.t('processingSection') }));
  for (const key of ['model_startup_timeout', 'model_response_header_timeout', 'model_stream_idle_timeout', 'model_request_timeout', 'audio_transcription_timeout', 'conspect_max_duration', 'conspect_idle_timeout']) {
    expect(screen.getByRole('textbox', { name: i18n.t(key) })).toHaveAccessibleDescription(i18n.t(`${key}Hint`));
    expect(i18n.t(key)).not.toBe(key);
  }
});

it('preserves expanded sections and edits across tabs and exposes collapsed validation errors', async () => {
  const user = userEvent.setup(); mount(); await user.click(await screen.findByRole('tab', { name: 'For developers' }));
  expect(screen.getAllByRole('tab')).toHaveLength(2);
  const section = screen.getByRole('button', { name: 'Text processing' });
  expect(section).toHaveAttribute('aria-expanded', 'false'); await user.click(section);
  const input = screen.getByRole('textbox', { name: 'Maximum batch collection time' });
  await user.clear(input); await user.type(input, '0s'); await user.click(section);
  expect(section).toHaveTextContent('Check the time limits');
  expect(screen.getByRole('button', { name: 'Save changes' })).toBeDisabled();
  await user.click(screen.getByRole('tab', { name: 'General' }));
  expect(screen.getByText(/A time limit is invalid/)).toBeVisible();
  await user.click(screen.getByRole('tab', { name: 'For developers' }));
  expect(section).toHaveAttribute('aria-expanded', 'false'); await user.click(section);
  expect(input).toHaveValue('0s');
  await user.click(screen.getByRole('button', { name: 'Cancel' })); expect(input).toHaveValue('5m');
  expect(section).not.toHaveTextContent('Check the time limits');
});

it('selects files in external mode and keeps help available', async () => {
  vi.spyOn(bridge, 'isDesktop').mockReturnValue(true);
  const picker = vi.spyOn(bridge, 'pickFile').mockResolvedValue('C:/runtime/llama-server.exe');
  const user = userEvent.setup(); mount(); await user.click(await screen.findByRole('tab', { name: 'For developers' }));
  await user.click(screen.getByRole('switch', { name: /Manage the language model locally/ }));
  expect(screen.getByText(/Start the server yourself/)).toBeVisible();
  const input = screen.getByRole('textbox', { name: 'Llama server executable' }); expect(input).toBeEnabled();
  await user.click(screen.getByRole('button', { name: 'Browse: Llama server executable' }));
  expect(picker).toHaveBeenCalledWith('Llama server executable'); expect(input).toHaveValue('C:/runtime/llama-server.exe');
  await user.click(screen.getByRole('button', { name: 'Help: Llama server executable' }));
  expect(screen.getByRole('dialog')).toBeVisible();
});
