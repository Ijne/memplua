from __future__ import annotations

import json
from pathlib import Path


def evaluate(dataset: Path) -> None:
    """Print structural quality metrics for JSON examples in a dataset tree."""
    total = valid = with_knowledge = 0
    for path in sorted(dataset.glob("*.json")):
        total += 1
        try:
            example = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            continue
        output = example.get("output")
        if not isinstance(example.get("input"), str) or not isinstance(output, dict):
            continue
        valid += 1
        candidates = output.get("terms", []) + output.get("thoughts", []) + output.get("entities", [])
        if candidates:
            with_knowledge += 1
    coverage = (with_knowledge / valid * 100) if valid else 0
    print(f"total={total} valid={valid} with_knowledge={with_knowledge} coverage={coverage:.1f}%")
