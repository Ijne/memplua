import json
import os
import shutil
from pathlib import Path

def clean_null_outputs(input_folder: str, backup_folder: str = None, dry_run: bool = True):
    """
    Удаляет JSON-файлы, у которых поле 'output' равно null.
    
    Args:
        input_folder: Папка с JSON-файлами для проверки
        backup_folder: (опционально) Папка для перемещения "плохих" файлов вместо удаления
        dry_run: Если True - только показывает, что будет сделано, без изменений
    """
    input_path = Path(input_folder)
    
    if not input_path.exists():
        print(f"❌ Папка {input_folder} не существует")
        return
    
    # Создаем папку для бэкапа, если указана
    if backup_folder:
        backup_path = Path(backup_folder)
        backup_path.mkdir(parents=True, exist_ok=True)
    
    json_files = list(input_path.glob("*.json"))
    total_files = len(json_files)
    bad_files = []
    good_files = []
    error_files = []
    
    print(f"📂 Найдено {total_files} JSON-файлов в {input_folder}")
    print("=" * 60)
    
    for json_file in json_files:
        try:
            with open(json_file, 'r', encoding='utf-8') as f:
                data = json.load(f)
            
            # Проверяем, есть ли поле 'output' и не равно ли оно null
            if models.get('output') is None:
                bad_files.append(json_file)
            else:
                good_files.append(json_file)
                
        except json.JSONDecodeError as e:
            print(f"⚠️  Ошибка парсинга {json_file.name}: {e}")
            error_files.append(json_file)
        except Exception as e:
            print(f"⚠️  Ошибка чтения {json_file.name}: {e}")
            error_files.append(json_file)
    
    # Выводим статистику
    print(f"\n📊 Статистика:")
    print(f"   ✅ Хорошие файлы (output есть): {len(good_files)}")
    print(f"   ❌ Плохие файлы (output = null): {len(bad_files)}")
    print(f"   ⚠️  Файлы с ошибками: {len(error_files)}")
    
    # Показываем примеры плохих файлов
    if bad_files:
        print(f"\n📋 Примеры плохих файлов (первые 5):")
        for f in bad_files[:5]:
            print(f"   - {f.name}")
        if len(bad_files) > 5:
            print(f"   ... и еще {len(bad_files) - 5} файлов")
    
    if dry_run:
        print("\n🔍 DRY RUN: изменения НЕ будут применены")
        print("   Чтобы применить, запустите с dry_run=False")
        return
    
    # Применяем изменения
    if backup_folder:
        # Перемещаем в бэкап
        for bad_file in bad_files:
            try:
                target = backup_path / bad_file.name
                shutil.move(str(bad_file), str(target))
                print(f"📦 Перемещен: {bad_file.name} → {backup_folder}")
            except Exception as e:
                print(f"❌ Ошибка перемещения {bad_file.name}: {e}")
    else:
        # Удаляем
        for bad_file in bad_files:
            try:
                bad_file.unlink()
                print(f"🗑️  Удален: {bad_file.name}")
            except Exception as e:
                print(f"❌ Ошибка удаления {bad_file.name}: {e}")
    
    print("\n✅ Готово!")
    print(f"   Осталось: {len(good_files)} файлов")
    print(f"   Удалено/перемещено: {len(bad_files)} файлов")

if __name__ == "__main__":
    # ======== НАСТРОЙКИ ========
    INPUT_FOLDER = r"./input.output"  # <-- Укажи путь к папке с JSON-файлами
    BACKUP_FOLDER = r"./backup/null_outputs"  # <-- Опционально: куда складывать плохие файлы
    DRY_RUN = True  # True = только показать, False = реально удалить
    # ============================
    
    clean_null_outputs(INPUT_FOLDER, BACKUP_FOLDER, DRY_RUN)