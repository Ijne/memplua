from __future__ import annotations

import json
from pathlib import Path


def train(dataset_path: Path, output: Path, model_name: str, epochs: float, max_sequence_length: int) -> None:
    """Fine-tune a causal language model with LoRA on prepared chat JSONL."""
    try:
        import torch
        from datasets import Dataset
        from peft import LoraConfig, get_peft_model
        from transformers import AutoModelForCausalLM, AutoTokenizer, DataCollatorForLanguageModeling, Trainer, TrainingArguments
    except ImportError as error:
        raise SystemExit("install research training dependencies with: pip install -e '.[train]'") from error

    rows = [json.loads(line) for line in dataset_path.read_text(encoding="utf-8").splitlines() if line.strip()]
    if len(rows) < 2:
        raise SystemExit("training requires at least two prepared examples")
    tokenizer = AutoTokenizer.from_pretrained(model_name, trust_remote_code=True)
    tokenizer.pad_token = tokenizer.pad_token or tokenizer.eos_token

    def tokenize(example: dict) -> dict:
        text = tokenizer.apply_chat_template(example["messages"], tokenize=False, add_generation_prompt=False)
        return tokenizer(text, truncation=True, max_length=max_sequence_length)

    dataset = Dataset.from_list(rows).map(tokenize, remove_columns=["messages"])
    split = dataset.train_test_split(test_size=max(1, len(dataset) // 10), seed=42)
    model = AutoModelForCausalLM.from_pretrained(
        model_name,
        torch_dtype=torch.float16 if torch.cuda.is_available() else torch.float32,
        device_map="auto",
        trust_remote_code=True,
    )
    model = get_peft_model(model, LoraConfig(
        r=16,
        lora_alpha=32,
        target_modules=["q_proj", "k_proj", "v_proj", "o_proj"],
        lora_dropout=0.05,
        bias="none",
        task_type="CAUSAL_LM",
    ))
    arguments = TrainingArguments(
        output_dir=str(output),
        num_train_epochs=epochs,
        per_device_train_batch_size=1,
        per_device_eval_batch_size=1,
        gradient_accumulation_steps=8,
        learning_rate=2e-4,
        logging_steps=10,
        save_steps=100,
        eval_strategy="steps",
        eval_steps=100,
        save_total_limit=3,
        fp16=torch.cuda.is_available(),
        report_to="none",
    )
    collator = DataCollatorForLanguageModeling(tokenizer=tokenizer, mlm=False)
    Trainer(
        model=model,
        args=arguments,
        train_dataset=split["train"],
        eval_dataset=split["test"],
        data_collator=collator,
    ).train()
    model.save_pretrained(output)
    tokenizer.save_pretrained(output)
