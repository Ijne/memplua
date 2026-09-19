from __future__ import annotations

import argparse
from pathlib import Path

from .clean_dataset import clean
from .evaluate import evaluate
from .prepare_dataset import prepare


def parser() -> argparse.ArgumentParser:
    """Build the contributor CLI and its dataset/training subcommands."""
    root = argparse.ArgumentParser(prog="memplua-research")
    commands = root.add_subparsers(dest="command", required=True)

    clean_command = commands.add_parser("clean", help="find invalid or null-output examples")
    clean_command.add_argument("dataset", type=Path)
    clean_command.add_argument("--quarantine", type=Path)
    clean_command.add_argument("--apply", action="store_true")
    clean_command.add_argument("--delete", action="store_true")

    prepare_command = commands.add_parser("prepare", help="create chat JSONL from raw examples")
    prepare_command.add_argument("dataset", type=Path)
    prepare_command.add_argument("output", type=Path)
    prepare_command.add_argument("--system-prompt", type=Path, required=True)

    evaluate_command = commands.add_parser("evaluate", help="report structural dataset quality")
    evaluate_command.add_argument("dataset", type=Path)

    train_command = commands.add_parser("train", help="train a LoRA adapter")
    train_command.add_argument("dataset", type=Path)
    train_command.add_argument("output", type=Path)
    train_command.add_argument("--model", required=True)
    train_command.add_argument("--epochs", type=float, default=3)
    train_command.add_argument("--max-sequence-length", type=int, default=4096)
    return root


def main() -> None:
    """Parse command-line arguments and dispatch the selected research action."""
    args = parser().parse_args()
    if args.command == "clean":
        clean(args.dataset, args.quarantine, apply=args.apply, delete=args.delete)
    elif args.command == "prepare":
        prepare(args.dataset, args.output, args.system_prompt)
    elif args.command == "evaluate":
        evaluate(args.dataset)
    elif args.command == "train":
        from .train_lora import train

        train(args.dataset, args.output, args.model, args.epochs, args.max_sequence_length)


if __name__ == "__main__":
    main()
