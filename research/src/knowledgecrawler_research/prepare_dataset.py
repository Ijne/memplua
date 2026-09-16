from __future__ import annotations

import json
from pathlib import Path


def prepare(dataset: Path, output: Path, system_prompt: Path) -> None:
    """Convert cleaned examples into chat-format JSONL for model training."""
    prompt = system_prompt.read_text(encoding="utf-8").strip()
    rows: list[str] = []
    for path in sorted(dataset.glob("*.json")):
        example = json.loads(path.read_text(encoding="utf-8"))
        if not isinstance(example.get("input"), str) or example.get("output") is None:
            continue
        row = {
            "messages": [
                {"role": "system", "content": prompt},
                {"role": "user", "content": example["input"]},
                {"role": "assistant", "content": json.dumps(example["output"], ensure_ascii=False)},
            ]
        }
        rows.append(json.dumps(row, ensure_ascii=False))
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text("\n".join(rows) + ("\n" if rows else ""), encoding="utf-8")
    print(f"prepared={len(rows)} output={output}")
