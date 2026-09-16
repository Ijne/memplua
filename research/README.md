# KnowledgeCrawler research

This directory is contributor-only. Production Go code neither imports nor launches it.

Create an isolated environment and install the CLI:

```powershell
py -m venv .venv
./.venv/Scripts/Activate.ps1
pip install -e .
```

Commands:

```powershell
knowledgecrawler-research clean datasets/raw --quarantine datasets/rejected
knowledgecrawler-research prepare datasets/raw datasets/prepared.jsonl --system-prompt prompt.txt
knowledgecrawler-research evaluate datasets/raw
pip install -e ".[train]"
knowledgecrawler-research train datasets/prepared.jsonl artifacts/lora --model Qwen/Qwen3-4B-Instruct
```

`clean` is a dry run unless `--apply` is supplied. With `--apply`, rejected files must be moved to a quarantine directory or deletion must be explicitly enabled with `--delete`. Datasets, checkpoints, and artifacts are intentionally excluded from Git.
