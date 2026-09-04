import json
import glob
import os
import torch
from datasets import Dataset
from transformers import (
    AutoModelForCausalLM,
    AutoTokenizer,
    TrainingArguments,
    Trainer,
    DataCollatorForLanguageModeling,
)
from peft import LoraConfig, get_peft_model, prepare_model_for_kbit_training

# ============================================================
# КОНФИГУРАЦИЯ
# ============================================================

BASE_MODEL = "Qwen/Qwen3-4B-Instruct"
DATASET_DIR = "input_output"
OUTPUT_DIR = "lora_output"
MAX_SEQ_LENGTH = 4096
LORA_R = 16
LORA_ALPHA = 32
LORA_DROPOUT = 0.05
BATCH_SIZE = 1
GRAD_ACCUM_STEPS = 8
LEARNING_RATE = 2e-4
NUM_EPOCHS = 3
WARMUP_RATIO = 0.03
LOGGING_STEPS = 10
SAVE_STEPS = 100

SYSTEM_PROMPT = """Ты — система построения графа знаний из транскриптов.

## Задача
Проанализируй текст и построй локальный граф знаний: выдели сущности (nodes) и связи между ними (edges).

## Типы узлов (используй только эти):
concept, technology, person, organization, process, property, event, location, object, kernel_mechanism, law, language, framework, protocol, measure, task, decision, problem, deadline

## Типы связей (используй только эти):
is_a, part_of, uses, causes, produces, contrasts_with, example_of, depends_on, implements, has_property, invented, discovered, located_in, related_to, assigned_to, blocks, solves, responsible_for

## Правила
1. Выделяй только значимые сущности (не местоимения, не общие слова).
2. Каждый узел должен иметь определение (definition).
3. Связи — только между существующими узлами.
4. Не выдумывай факты, которых нет в тексте.
5. Если связь неочевидна — используй "related_to" или не указывай её.
6. Ответ — строго JSON.
7. Ответ должен содержать НЕ БОЛЕЕ 15 узлов и НЕ БОЛЕЕ 25 рёбер. Чем меньше, тем лучше, выделены должны быть важнейшие узлы, всё зависит от плотности важной информации.
8. Definitions — не длиннее 100 слов. Чем короче, тем лучше, не нужно стремиться к более длинному определению.
9. key_facts — не более 2 на узел, каждое не длиннее 30 слов.
10. Evidence — не длиннее 30 слов.
11. Не добавляй узлы, которые не являются ключевыми для понимания темы.

## Дополнительные правила для рабочих совещаний
12. Если текст — это рабочее совещание или планерка:
    - Обязательно выдели людей (person) и задачи (task).
    - Для каждой задачи укажи ответственного через связь assigned_to.
    - Если задача блокируется проблемой — связь blocks.
    - Если задача зависит от другой задачи — связь depends_on.
    - Решения выделяй как decision, сроки как deadline."""


# ============================================================
# ЗАГРУЗКА ДАТАСЕТА
# ============================================================

def load_examples(dataset_dir: str):
    """Загружает все примеры из input_output/*.json"""
    examples = []

    files = glob.glob(os.path.join(dataset_dir, "*.json"))
    print(f"Найдено файлов: {len(files)}")

    for file in files:
        with open(file, "r", encoding="utf-8") as f:
            try:
                example = json.load(f)
            except json.JSONDecodeError:
                print(f"Пропуск (невалидный JSON): {file}")
                continue

        # Пропускаем пустые
        if example.get("output") is None:
            print(f"Пропуск (output=null): {file}")
            continue

        output = example["output"]
        if len(output.get("nodes", [])) == 0:
            print(f"Пропуск (нет узлов): {file}")
            continue

        # Формируем сообщения в формате chat
        messages = [
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": example["input"]},
            {"role": "assistant", "content": json.dumps(output, ensure_ascii=False)},
        ]

        examples.append({"messages": messages})

    print(f"Загружено примеров: {len(examples)}")
    return examples


# ============================================================
# ФОРМАТИРОВАНИЕ
# ============================================================

def format_chat_template(example, tokenizer):
    """Преобразует сообщения в строку для обучения"""
    text = tokenizer.apply_chat_template(
        example["messages"],
        tokenize=False,
        add_generation_prompt=False,
    )
    return {"text": text}


# ============================================================
# ОБУЧЕНИЕ
# ============================================================

def train():
    # Проверка GPU
    if torch.cuda.is_available():
        print(f"GPU: {torch.cuda.get_device_name(0)}")
        print(f"VRAM: {torch.cuda.get_device_properties(0).total_memory / 1e9:.1f} GB")
    else:
        print("GPU не найден, будет использоваться CPU (медленно)")

    # Загрузка токенизатора
    print(f"Загружаю токенизатор: {BASE_MODEL}")
    tokenizer = AutoTokenizer.from_pretrained(BASE_MODEL, trust_remote_code=True)
    tokenizer.pad_token = tokenizer.eos_token

    # Загрузка модели
    print(f"Загружаю модель: {BASE_MODEL}")
    model = AutoModelForCausalLM.from_pretrained(
        BASE_MODEL,
        torch_dtype=torch.float16,
        device_map="auto",
        trust_remote_code=True,
    )

    # Подготовка для LoRA
    model = prepare_model_for_kbit_training(model)

    # LoRA конфиг
    lora_config = LoraConfig(
        r=LORA_R,
        lora_alpha=LORA_ALPHA,
        target_modules=["q_proj", "k_proj", "v_proj", "o_proj"],
        lora_dropout=LORA_DROPOUT,
        bias="none",
        task_type="CAUSAL_LM",
    )

    model = get_peft_model(model, lora_config)
    model.print_trainable_parameters()

    # Загрузка датасета
    examples = load_examples(DATASET_DIR)
    if len(examples) == 0:
        raise ValueError("Нет примеров для обучения")

    dataset = Dataset.from_list(examples)
    dataset = dataset.map(
        lambda x: format_chat_template(x, tokenizer),
        remove_columns=["messages"],
    )

    # Разделение на train/val
    split = dataset.train_test_split(test_size=0.1, seed=42)
    train_dataset = split["train"]
    val_dataset = split["test"]

    print(f"Train: {len(train_dataset)} примеров")
    print(f"Val: {len(val_dataset)} примеров")

    # Аргументы обучения
    training_args = TrainingArguments(
        output_dir=OUTPUT_DIR,
        per_device_train_batch_size=BATCH_SIZE,
        per_device_eval_batch_size=1,
        gradient_accumulation_steps=GRAD_ACCUM_STEPS,
        learning_rate=LEARNING_RATE,
        num_train_epochs=NUM_EPOCHS,
        warmup_ratio=WARMUP_RATIO,
        logging_steps=LOGGING_STEPS,
        save_steps=SAVE_STEPS,
        evaluation_strategy="steps",
        eval_steps=SAVE_STEPS,
        save_total_limit=3,
        fp16=torch.cuda.is_available(),
        report_to="none",
        remove_unused_columns=False,
        gradient_checkpointing=True,
    )

    # Коллатор
    data_collator = DataCollatorForLanguageModeling(
        tokenizer=tokenizer,
        mlm=False,
    )

    # Тренер
    trainer = Trainer(
        model=model,
        args=training_args,
        train_dataset=train_dataset,
        eval_dataset=val_dataset,
        data_collator=data_collator,
    )

    # Обучение
    print("Начинаю обучение...")
    trainer.train()

    # Сохранение
    print(f"Сохраняю модель в {OUTPUT_DIR}")
    model.save_pretrained(OUTPUT_DIR)
    tokenizer.save_pretrained(OUTPUT_DIR)

    print("Готово!")


if __name__ == "__main__":
    train() 