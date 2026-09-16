from __future__ import annotations

import json
import shutil
from pathlib import Path


def clean(dataset: Path, quarantine: Path | None, *, apply: bool, delete: bool) -> None:
    """Report invalid dataset files and optionally quarantine or delete them."""
    if not dataset.is_dir():
        raise SystemExit(f"dataset directory does not exist: {dataset}")
    if apply and quarantine is None and not delete:
        raise SystemExit("--apply requires --quarantine or the explicit --delete option")
    rejected: list[tuple[Path, str]] = []
    files = sorted(dataset.glob("*.json"))
    for path in files:
        try:
            value = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as error:
            rejected.append((path, f"invalid JSON: {error}"))
            continue
        if value.get("output") is None:
            rejected.append((path, "output is null or missing"))

    for path, reason in rejected:
        print(f"reject {path.name}: {reason}")
        if not apply:
            continue
        if quarantine is not None:
            quarantine.mkdir(parents=True, exist_ok=True)
            shutil.move(str(path), quarantine / path.name)
        else:
            path.unlink()
    print(f"checked={len(files)} rejected={len(rejected)} applied={apply}")
