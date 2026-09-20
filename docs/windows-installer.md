# Windows installer

The Windows package is a per-user Inno Setup installer. It does not request
administrator privileges and installs the application below
`%LocalAppData%\Programs\memplua`.

Runtime state is deliberately separate from installed files:

- `%AppData%\memplua\config.toml` — settings;
- `%LocalAppData%\memplua` — SQLite, logs, API token and exports;
- the installation directory — executable, native runtime, local models.

Updating or uninstalling the application therefore does not remove the user's
knowledge graph. The application discovers models from the installer layout
when no explicit model paths are configured.

## Build the offline installer

The setup executable contains `memplua.exe`, the native runtime and all models
required for the first launch. Installing and running the application does not
require a network connection. The bundled components are:

- the latest SHA-256-attested Windows CPU release of [llama.cpp](https://github.com/ggml-org/llama.cpp/releases);
- [Qwen3-4B Q4_K_M](https://huggingface.co/Qwen/Qwen3-4B-GGUF), published by Qwen;
- [Whisper small Q5_1](https://huggingface.co/ggerganov/whisper.cpp), published by the whisper.cpp maintainer;
- [Silero VAD](https://github.com/snakers4/silero-vad), published by Silero;
- the latest SHA-256-attested Windows CPU release of [ONNX Runtime](https://github.com/microsoft/onnxruntime/releases).

The application discovers this installed layout automatically. User databases,
logs, configuration and exported notes are never included in the installer.

```powershell
.\scripts\release\build-windows-installer.ps1
```

Inno Setup 6 is required. Optional `-VCRedist` and
`-WebView2Bootstrapper` arguments bundle the corresponding Microsoft runtime
installers. Release output is written to `dist/windows` as
`memplua-<version>-windows-x64-offline-setup.exe` and must not be committed.

Before public distribution, inventory and ship the licenses required by the
selected Qwen LLM, llama.cpp runtime, Whisper, Silero, ONNX Runtime and compiler runtime.
Sign the final setup executable with an Authenticode certificate to avoid an
untrusted-publisher warning.
