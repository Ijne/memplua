import { useState } from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import i18n from '../../lib/i18n';
import { isDesktop, openExternalURL } from '../../lib/bridge';
import { FileHelp, type FileHelpKey } from './FileHelp';

vi.mock('../../lib/bridge', () => ({ isDesktop: vi.fn(() => true), openExternalURL: vi.fn() }));
function Harness({ field }: { field: FileHelpKey }) {
  const [open, setOpen] = useState(false);
  return <><button onClick={() => setOpen(true)}>Help</button>{open && <FileHelp field={field} onClose={() => setOpen(false)} />}</>;
}
beforeEach(async () => { vi.clearAllMocks(); vi.mocked(isDesktop).mockReturnValue(true); vi.mocked(openExternalURL).mockResolvedValue(); await i18n.changeLanguage('en'); });
it('traps keyboard focus and returns to the opener after Escape and close', async () => {
  const user = userEvent.setup(); render(<Harness field="llama_binary" />);
  const opener = screen.getByRole('button', { name: 'Help' }); await user.click(opener);
  const close = screen.getByRole('button', { name: 'Close' }); expect(close).toHaveFocus();
  await user.tab({ shift: true }); expect(screen.getByRole('link', { name: 'llama-server arguments' })).toHaveFocus();
  await user.tab(); expect(close).toHaveFocus();
  await user.keyboard('{Escape}'); expect(screen.queryByRole('dialog')).not.toBeInTheDocument(); expect(opener).toHaveFocus();
  await user.click(opener); await user.click(screen.getByRole('button', { name: 'Close' })); expect(opener).toHaveFocus();
});
it('opens native sources, reports errors locally, and retries', async () => {
  const user = userEvent.setup(); render(<Harness field="onnx_runtime" />); await user.click(screen.getByRole('button', { name: 'Help' }));
  const link = screen.getByRole('link', { name: 'ONNX Runtime releases' });
  vi.mocked(openExternalURL).mockRejectedValueOnce(new Error('native details'));
  await user.click(link); expect(await screen.findByRole('alert')).toHaveTextContent('Could not open the link');
  expect(screen.queryByText('native details')).not.toBeInTheDocument();
  await user.click(link); await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument());
  expect(openExternalURL).toHaveBeenCalledWith('https://github.com/microsoft/onnxruntime/releases');
});
it('keeps regular external links in browser preview', async () => {
  vi.mocked(isDesktop).mockReturnValue(false); render(<Harness field="llm_model" />);
  await userEvent.setup().click(screen.getByRole('button', { name: 'Help' }));
  const link = screen.getByRole('link', { name: 'Models on Hugging Face' });
  expect(link).toHaveAttribute('href', 'https://huggingface.co/models'); expect(link).toHaveAttribute('target', '_blank');
  expect(link).toHaveAttribute('rel', 'noopener noreferrer');
  expect(openExternalURL).not.toHaveBeenCalled();
});
for (const language of ['en', 'ru']) for (const field of ['llama_binary', 'llm_model', 'whisper_model', 'silero_model', 'onnx_runtime'] as const) it(`explains ${field} in ${language}`, async () => {
  await i18n.changeLanguage(language); render(<Harness field={field} />); await userEvent.setup().click(screen.getByRole('button', { name: 'Help' }));
  expect(screen.getByRole('dialog')).toHaveAccessibleName(i18n.t(field));
  expect(screen.getByRole('dialog')).toHaveAccessibleDescription(i18n.t(`${field}Help`));
  expect(screen.getByText(i18n.t(`${field}Help`))).not.toHaveTextContent(`${field}Help`);
  expect(screen.getAllByRole('link').length).toBeGreaterThan(0);
});
