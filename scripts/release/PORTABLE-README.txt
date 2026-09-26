memplua portable for Windows x64

This package contains memplua, its required MinGW runtime DLLs, ONNX Runtime,
and llama.cpp runtime. It does not contain language or speech models.

1. Extract the entire archive to a writable folder.
2. Open PowerShell in that folder.
3. Run:

   powershell -ExecutionPolicy Bypass -File .\download-models.ps1

The script downloads these public artifacts from their official publishers and
verifies their checksums before installing them under models\:

- Qwen3-4B-Q4_K_M.gguf
- ggml-small-q5_1.bin
- Silero VAD ONNX model

After the script completes, start memplua.exe. To store models elsewhere, pass
an absolute directory path:

   powershell -ExecutionPolicy Bypass -File .\download-models.ps1 -AssetsDirectory D:\memplua-models

Do not move or delete the DLL files beside memplua.exe or the files in runtime\.
