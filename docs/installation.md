# Install memplua on Windows 11 (x64)

Download the full offline installer from the [offline-installer](https://memplua.space/](https://huggingface.co/ijne/memplua/blob/main/memplua-0.2.0-alpha.4-windows-x64-offline-setup.exe). The portable ZIP and checksums are published under [GitHub Releases](https://github.com/Ijne/memplua/releases). Do not download `Source code (zip)` or a standalone `memplua.exe`: source code is not an installed application, and the EXE needs additional libraries and models.

If the website does not yet offer the offline installer, or the release has no `*-windows-x64-portable.zip` under **Assets**, that package has not been published yet. The installer and portable ZIP should have the same version.

## Option 1: Full offline installer

1. Open [memplua.space](https://memplua.space/) and use its installer download button. It leads to the file on Hugging Face. Download `memplua-<version>-windows-x64-offline-setup.exe` (about 2.6 GB); it includes Qwen3-4B-Q4, Whisper small, Silero, and the required libraries.
2. Run the downloaded file and choose an installation folder. Installation itself does not require an internet connection.
3. Open **memplua** from the Start menu, or from the desktop shortcut if you created one.

By default, the application is installed in `%LOCALAPPDATA%\Programs\memplua`. Settings and personal data are stored separately in your Windows profile.

## Option 2: Portable ZIP

1. Download `memplua-<version>-windows-x64-portable.zip` from **Assets** of the matching [GitHub release](https://github.com/Ijne/memplua/releases). Extract the **entire folder** to a writable location. Do not run the EXE from inside the archive viewer.
2. Open PowerShell in the extracted folder containing `memplua.exe` and `download-models.ps1`.
3. Run:

   ```powershell
   powershell -NoProfile -ExecutionPolicy Bypass -File .\download-models.ps1
   ```

4. Wait for the download to finish, then run `memplua.exe` from that folder.

The ZIP includes the application, required DLLs, ONNX Runtime, and llama.cpp, but not the models. The script downloads Qwen3-4B-Q4, Whisper small, and Silero from their official publishers, verifies their checksums, and places them under `models\`. This first download requires internet access and about 3 GB of free disk space. Keep the DLLs beside `memplua.exe` and keep the `runtime\` folder.

To store models elsewhere, provide an absolute path:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\download-models.ps1 -AssetsDirectory 'D:\memplua-models'
```

The script writes those paths to your user configuration. Once the models are downloaded, normal startup does not require internet access.

## Verify the downloaded files

Download `SHA256SUMS.txt` from **Assets** of the same GitHub release. It contains checksums for both the installer hosted on the website and the portable ZIP hosted on GitHub. In PowerShell, run:

```powershell
(Get-FileHash -Algorithm SHA256 '.\memplua-<version>-windows-x64-offline-setup.exe').Hash.ToLowerInvariant()
```

For the portable ZIP, substitute `memplua-<version>-windows-x64-portable.zip`. Compare the output with the matching line in `SHA256SUMS.txt`. If they differ, download the file again.

## If it does not start

- Error mentioning `libgomp-1.dll`, `libstdc++-6.dll`, or `libwinpthread-1.dll`: use the full installer or extract the complete portable ZIP again. These DLLs must be next to `memplua.exe`.
- Portable version reports a missing model: run `download-models.ps1` and wait for it to finish. Details are in `model-install.log` beside the EXE.
- For other issues, see [Troubleshooting](troubleshooting.md) and [open an issue](https://github.com/Ijne/memplua/issues) with your Windows version, release filename, and exact error message. Review logs before sharing them to avoid posting private data.

[Русская версия](installation.ru.md)
