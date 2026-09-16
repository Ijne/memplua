export type SourceState = 'off' | 'starting' | 'recording' | 'paused' | 'failed';
export interface UISource { id: string; kind: string; available: boolean; state: SourceState; session_id?: string; code?: string; actions: string[]; started_at?: string; stopped_at?: string }
export interface UserNotice { code: string; params?: Record<string, unknown>; severity?: string; action?: string | { type?: string; kind?: string; target?: string }; audience: 'user' }
export interface UIState { application: string; sources: UISource[]; processing: 'idle' | 'working' | 'retrying' | 'paused' | 'failed'; pending_conspects: number; pending_items: number; warning?: UserNotice }
export interface Settings {
  ui: { language: 'ru' | 'en'; theme: 'system' | 'light' | 'dark'; always_on_top: boolean; start_with_windows: boolean };
  conspect: { language: string };
  language: string;
  export: { obsidian_directory: string; auto: boolean };
  models: { managed: boolean; llama_binary: string; llm_model: string; llm_url: string; parallel: number; gpu_layers: number; server_args: string[]; whisper_model: string; silero_model: string; onnx_runtime: string };
  logging: { level: string };
  pipeline: { processing_limit: number; review_limit: number };
}
