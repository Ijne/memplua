import type { Conspect, Tag } from '../features/review/types';
import type { Settings, UIState } from '../lib/types';
export const tags: Tag[] = [{ id: 'tag-1', name: 'Distributed systems', aliases: ['systems'], enabled: true }, { id: 'tag-2', name: 'Architecture', aliases: [], enabled: true }];
export const conspect: Conspect = {
  id: 'note-1', topic: 'Designing systems that stay reliable', created_at: '2026-09-12T08:30:00Z', status: 'review_pending', source_kind: 'audio/microphone', item_count: 2, unresolved_items: 2, unresolved_tags: 0,
  items: [
    { id: 'item-1', kind: 'term', status: 'pending', resolution: '', target_version: 0,
      incoming_variants: [{ candidate_id: 'candidate-1', value: { name: 'Eventual consistency', description: 'A consistency model in which all replicas converge to the same state when no new updates are made. It allows a system to remain available while changes travel between nodes.', tags: ['Distributed systems'] } }],
      canonical_matches: [{ id: 'canonical-1', kind: 'term', source: 'canonical', exact: true, version: 2, value: { name: 'Eventual consistency', description: 'Replicas become consistent over time, without requiring every read to return the latest write.', tags: ['Distributed systems', 'Architecture'] } }],
      pending_matches: [{ id: 'pending-1', kind: 'term', source: 'pending', exact: false, version: 0, value: { name: 'Consistency across replicas', description: 'An idea from another note, still waiting for review.', tags: ['Architecture'] } }],
      final_value: { name: 'Eventual consistency', description: 'A consistency model in which replicas converge to the same state over time.', tags: ['Distributed systems'] } },
    { id: 'item-2', kind: 'thought', status: 'pending', resolution: '', target_version: 0, related_item_ids: ['item-1'], incoming_variants: [{ candidate_id: 'candidate-2', value: { thesis: 'Choose consistency for the user’s task', body: 'Different actions need different guarantees. A shopping cart can tolerate a short delay; the final payment needs a stronger guarantee.', tags: ['Architecture'] }, related_terms: ['Eventual consistency'] }], canonical_matches: [], pending_matches: [], final_value: { thesis: 'Choose consistency for the user’s task', body: 'Different actions need different guarantees.', tags: ['Architecture'] } },
  ], taxonomy: [],
};
export const settings: Settings = {
  ui: { language: 'en', theme: 'light', always_on_top: true, start_with_windows: false }, conspect: { language: 'en' }, language: 'en',
  export: { obsidian_directory: '', auto: false }, models: { managed: true, llama_binary: '', llm_model: '', whisper_model: '', silero_model: '', onnx_runtime: '', llm_url: 'http://127.0.0.1:8081', parallel: 1, gpu_layers: 0, server_args: [] }, logging: { level: 'info' }, pipeline: { processing_limit: 1000, review_limit: 500 },
};
export const uiState: UIState = { application: 'running', sources: [{ id: 'microphone', kind: 'audio/microphone', state: 'off', available: true, actions: ['start'] }, { id: 'loopback', kind: 'audio/loopback', state: 'off', available: true, actions: ['start'] }], processing: 'idle', pending_conspects: 1, pending_items: 2 };
