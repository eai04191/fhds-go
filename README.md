# fhds-go

DualSense adaptive triggers for Forza Horizon on Windows.
Go port (headless / Windows-only) of [HamzaYslmn/Forza-Horizon-DualSense-Python](https://github.com/HamzaYslmn/Forza-Horizon-DualSense-Python).

## Build

```powershell
go build -o bin/fhds.exe ./cmd/fhds
```

## Forza settings

**Settings → HUD and Gameplay**, at the bottom:

| Option              | Value     |
| ------------------- | --------- |
| Data Out            | ON        |
| Data Out IP Address | 127.0.0.1 |
| Data Out IP Port    | 5300      |

## Run

```powershell
.\bin\fhds.exe
```

Both triggers briefly stiffen on launch once the DualSense is recognized.

## Tested with

- Forza Horizon 6 (Xbox / Microsoft Store version)
- [Official DS4Windows v3.5](https://ds4-windows.com/download/official/)

## License

AGPL-3.0-or-later (inherited from [upstream](https://github.com/HamzaYslmn/Forza-Horizon-DualSense-Python)). See [LICENSE](LICENSE).
