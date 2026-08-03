@echo off
setlocal
chcp 65001 >nul

rem 清除 ESP32-S3 MaClaw 客户端保存的 Wi-Fi、配对码及网关令牌。
rem 不会擦除应用固件、屏幕驱动或语音模型。
set "ESP_PYTHON=C:\Espressif\tools\python\v6.0.2\venv\Scripts\python.exe"
set "ESPTOOL=C:\esp\v6.0.2\esp-idf\components\esptool_py\esptool\esptool.py"
set "PORT=COM3"

if not exist "%ESP_PYTHON%" (
  echo 未找到 ESP-IDF 6.0.2 Python 环境：%ESP_PYTHON%
  pause
  exit /b 1
)

echo 正在清除 %PORT% 上的设备配置...
"%ESP_PYTHON%" "%ESPTOOL%" --chip esp32s3 --port %PORT% erase-region 0x9000 0x6000
if errorlevel 1 (
  echo 清除失败。请确认设备已开机、USB 已连接，并且没有其它串口监视器占用 %PORT%。
  pause
  exit /b 1
)

echo.
echo 已清除配置，设备已自动重启。
echo 请连接设备热点 MACLAW-SETUP-xxxx，然后访问 http://192.168.4.1 完成设置。
pause
