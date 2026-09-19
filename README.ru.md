# memplua

memplua — локальное Windows-приложение, которое превращает речь и текст
в граф знаний с обязательным подтверждением пользователя.

> Статус проекта: **Alpha v2 (`0.2.0-alpha.2`)**. Основной pipeline, review,
> desktop-интерфейс и компактный установщик с проверяемой загрузкой моделей уже
> работают; длительные Windows-тесты и подготовка публичного релиза продолжаются.

```text
звук или текст -> chunks в SQLite -> analysis batch -> локальная LLM
-> тематические конспекты -> review -> атомарное изменение графа -> exporters
```

LLM не изменяет эталонную базу напрямую. Она только выделяет кандидатов — термины
и мысли с provenance до исходных текстовых chunks. Пользователь выбирает для
каждой сущности `create`, `update`, `keep existing` или `reject`, после чего весь
конспект применяется одной транзакцией.

## Установка на Windows

Запустите `memplua-0.2.0-alpha.2-windows-x64-setup.exe`. Установщик работает в
профиле текущего пользователя, скачивает локальные модели с официальных
источников и проверяет их перед установкой. Первичная загрузка — около 2,8 ГБ.
Подробности находятся в [документации установщика](docs/windows-installer.md).

## Сборка и запуск из исходников

```powershell
Copy-Item config.example.toml config.toml

powershell.exe -NoProfile -ExecutionPolicy Bypass `
  -File ./scripts/dev/build-desktop.ps1 `
  -Native -WhisperRoot C:/path/to/whisper.cpp

./memplua.exe --config ./config.toml
```

Desktop не требует ручного ввода API-ключа. Внутренний одноразовый credential
передаётся интерфейсу автоматически и живёт только до завершения процесса.
Token-файл относится только к браузерной developer-панели команды `serve`.

## Где читать о проекте

Основная техническая документация ведётся на английском, чтобы репозиторий был
доступен международным contributors:

- [карта документации](docs/README.md);
- [архитектура и границы пакетов](docs/architecture.md);
- [полный pipeline и восстановление](docs/pipeline.md);
- [модель данных и SQLite](docs/data-model.md);
- [конфигурация](docs/configuration.md);
- [runtime, desktop, API и shutdown](docs/runtime.md);
- [справочник по коду и функциям](docs/code-reference.md);
- [разработка и тестирование](docs/development.md);
- [диагностика проблем](docs/troubleshooting.md);
- [privacy и security](docs/privacy-security.md).
- [изменения релизов](CHANGELOG.md).

Краткий список главных инвариантов для разработки находится в
[CONTRIBUTING.md](CONTRIBUTING.md), а обоснования решений — в [docs/adr](docs/adr/).

Перед публичной публикацией необходимо выбрать лицензию и добавить файл
`LICENSE`; сейчас права формально остаются у автора.
