# memplua research

This directory is contributor-only. Production Go code neither imports nor launches it.

Create an isolated environment and install the CLI:

```powershell
py -m venv .venv
./.venv/Scripts/Activate.ps1
pip install -e .
```

Commands:

```powershell
memplua-research clean datasets/raw --quarantine datasets/rejected
memplua-research prepare datasets/raw datasets/prepared.jsonl --system-prompt prompt.txt
memplua-research evaluate datasets/raw
pip install -e ".[train]"
memplua-research train datasets/prepared.jsonl artifacts/lora --model Qwen/Qwen3-4B-Instruct
```

`clean` is a dry run unless `--apply` is supplied. With `--apply`, rejected files must be moved to a quarantine directory or deletion must be explicitly enabled with `--delete`. Datasets, checkpoints, and artifacts are intentionally excluded from Git.
