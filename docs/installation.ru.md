# Установка memplua на Windows 11 (x64)

Полный офлайн-установщик скачивается с [оффлайн установщик](https://huggingface.co/ijne/memplua/resolve/main/memplua-0.2.0-alpha.4-windows-x64-offline-setup.exe). Portable ZIP и контрольные суммы публикуются в [GitHub Releases](https://github.com/Ijne/memplua/releases). Не скачивайте `Source code (zip)` и отдельный `memplua.exe`: исходный код не является готовой программой, а одному EXE не хватает библиотек и моделей.

Если на сайте ещё нет офлайн-установщика или в Assets релиза нет `*-windows-x64-portable.zip`, соответствующий пакет ещё не опубликован. Версии установщика и portable ZIP должны совпадать.

## Вариант 1: полный офлайн-установщик

1. Откройте [memplua.space](https://memplua.space/) и нажмите кнопку скачивания установщика. Она ведёт к файлу на Hugging Face. Скачайте `memplua-<версия>-windows-x64-offline-setup.exe` (около 2,6 ГБ); он содержит Qwen3-4B-Q4, Whisper small, Silero и необходимые библиотеки.
2. Запустите скачанный файл и выберите папку установки. Для самой установки интернет не нужен.
3. После установки запустите **memplua** из меню «Пуск» или с рабочего стола, если создавали ярлык.

Программа по умолчанию устанавливается в `%LOCALAPPDATA%\Programs\memplua`. Настройки и пользовательские данные хранятся отдельно в профиле Windows.

## Вариант 2: portable ZIP

1. Скачайте `memplua-<версия>-windows-x64-portable.zip` из **Assets** соответствующего [релиза на GitHub](https://github.com/Ijne/memplua/releases) и распакуйте **всю папку** в место, где у вас есть право записи. Не запускайте EXE прямо из окна архиватора.
2. Откройте PowerShell в распакованной папке, где находятся `memplua.exe` и `download-models.ps1`.
3. Выполните:

   ```powershell
   powershell -NoProfile -ExecutionPolicy Bypass -File .\download-models.ps1
   ```

4. Дождитесь завершения загрузки и запустите `memplua.exe` из этой же папки.

ZIP содержит приложение, необходимые DLL, ONNX Runtime и llama.cpp, но не содержит модели. Скрипт загружает Qwen3-4B-Q4, Whisper small и Silero из официальных источников, проверяет их контрольные суммы и кладёт в `models\`. Для этой первой загрузки нужен интернет и около 3 ГБ свободного места. Не перемещайте `memplua.exe` отдельно от DLL и папки `runtime\`.

Если модели нужно хранить в другом месте, укажите полный путь:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\download-models.ps1 -AssetsDirectory 'D:\memplua-models'
```

Скрипт запишет выбранные пути в пользовательский конфиг. После загрузки моделей для обычного запуска интернет не нужен.

## Проверка скачанных файлов

Скачайте `SHA256SUMS.txt` из Assets того же релиза GitHub, что и ваш пакет. Файл содержит хеши установщика с сайта и portable ZIP с GitHub. В PowerShell выполните для нужного файла:

```powershell
(Get-FileHash -Algorithm SHA256 '.\memplua-<версия>-windows-x64-offline-setup.exe').Hash.ToLowerInvariant()
```

Для portable ZIP замените имя файла на `memplua-<версия>-windows-x64-portable.zip`. Полученное значение должно совпасть со строкой этого файла в `SHA256SUMS.txt`. Если не совпало, скачайте файл повторно.

## Если запуск не удался

- Ошибка `libgomp-1.dll`, `libstdc++-6.dll` или `libwinpthread-1.dll`: используйте полный установщик либо распакуйте portable ZIP заново целиком. Эти DLL должны лежать рядом с `memplua.exe`.
- Portable-версия сообщает об отсутствии модели: запустите `download-models.ps1` и дождитесь его завершения. Подробности ошибки находятся в `model-install.log` рядом с EXE.
- Другие проблемы: см. [руководство по диагностике](troubleshooting.md) и [создайте issue](https://github.com/Ijne/memplua/issues), указав версию Windows, имя релизного файла и точный текст ошибки. Не прикладывайте личные данные или полный лог без просмотра.

[English version](installation.md)
