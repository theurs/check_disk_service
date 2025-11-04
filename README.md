# Disk Monitor Service

Disk Monitor Service is a lightweight, background Windows service written in Go that proactively monitors the health of your hard drives (HDDs) and solid-state drives (SSDs). It leverages PowerShell to track critical S.M.A.R.T. attributes and sends stateful alerts via Telegram only when a disk's health status changes.

## Key Features

*   **Stateful Alerts**: Avoids alert fatigue by only sending notifications when the disk status *changes*—when a new error appears, an existing one worsens, or an issue is resolved.
*   **Robust PowerShell-Based Health Checks**: Instead of the limited `wmic` utility, it uses PowerShell's `Get-StorageReliabilityCounter` to track reliable predictors of drive failure for both HDDs and SSDs:
    *   **`Wear`** (for SSDs): Tracks the percentage of the drive's lifespan that has been consumed.
    *   `ReallocatedSectors`: Indicates sectors that have been moved due to read/write errors.
    *   `CurrentPendingSectors`: "Unstable" sectors awaiting remapping.
    *   `ReadErrorsUncorrected`: Critical errors that could not be corrected.
*   **Telegram Notifications**: Get instant, informative alerts with clear Markdown formatting delivered directly to your Telegram account.
*   **External JSON Configuration**: Easily change your Bot Token and Chat ID in a `config.json` file without recompiling the service.
*   **Automatic Config Generation**: On its first run, the service automatically creates a `config.json` template for you to fill out.
*   **Built-in Log Rotation**: Log files are automatically managed (compressed, archived, and deleted) to prevent them from consuming excessive disk space.
*   **Standard Windows Service Management**: Installs and runs as a native Windows service, manageable via `net start`/`stop` or the Services GUI.
*   **Retry Mechanism for Notifications**: If a network error occurs, the service will attempt to resend the notification with an exponential backoff delay.

## Prerequisites

*   **Windows OS**: The service is designed exclusively for Windows.
*   **PowerShell**: Must be available and enabled (default on modern Windows versions).
*   **Go**: Required for compiling the project from source (version 1.18+ recommended).

## Getting Started

### Step 1: Clone the Repository

Clone or download the source code to a local directory.

```shell
git clone <your-repository-url>
cd <project-directory>
```

### Step 2: Configure Telegram Alerts

The service requires a Telegram Bot Token and a Chat ID to send notifications.

1.  **Create a Telegram Bot:**
    *   In Telegram, search for the `BotFather` bot.
    *   Send the `/newbot` command and follow the prompts.
    *   BotFather will provide you with a unique **Bot Token**. Copy and save it.

2.  **Get Your Chat ID:**
    *   In Telegram, search for the `userinfobot` bot.
    *   Send the `/start` command.
    *   The bot will reply with your **Chat ID**. Copy and save it.

3.  **Generate and Edit `config.json`:**
    *   First, build a temporary version of the application: `go build -o DiskMonitorService.exe main.go`
    *   Run the executable from your command line: `DiskMonitorService.exe`
    *   The service will detect the missing file, create a template named `config.json`, print a message, and then gracefully exit. The generated file will look like this:
      ```json
      {
        "telegram_token": "YOUR_TOKEN_HERE",
        "telegram_chat_id": "YOUR_CHAT_ID_HERE"
      }
      ```
    *   Open `config.json` in a text editor and replace the placeholder values with your actual Bot Token and Chat ID.

### Step 3: Build the Service

Open a command prompt in the project directory and run the final build command:

```shell
go build -ldflags="-w -s -H=windowsgui" -o DiskMonitorService.exe main.go
```
*   The `-ldflags` reduce the final binary size and compile it as a background application (no console window).

## Deployment & Usage

Place the compiled `DiskMonitorService.exe` and the completed `config.json` file into a permanent directory, such as `C:\Program Files\DiskMonitorService`.

> **Important**: All commands below must be run from a command prompt or PowerShell with **Administrator privileges**.

### Testing

Before installing the service, you can run a one-time check. This command will perform a health scan and always send a summary notification to Telegram, confirming that your configuration is correct.

```shell
DiskMonitorService.exe test
```

### Managing the Service

*   **Install the Service:**
    ```shell
    DiskMonitorService.exe install
    ```
*   **Start the Service:**
    ```shell
    net start DiskMonitorService
    ```
*   **Stop the Service:**
    ```shell
    net stop DiskMonitorService
    ```
*   **Remove the Service:**
    ```shell
    DiskMonitorService.exe remove
    ```

You can also manage the service through the Windows Services administrative tool (`services.msc`).

## Logging

The service maintains a log file named `DiskMonitorService.log` in the same directory as the executable. Log rotation is handled automatically by the `lumberjack` library with the following settings:

*   **Max Size**: 10 MB per log file.
*   **Max Backups**: 5 old log files are kept.
*   **Max Age**: Log files are kept for a maximum of 30 days.
*   **Compression**: Rotated log files are compressed into `.gz` format.


---


# Сервис Мониторинга Дисков (Disk Monitor Service)

Это фоновая служба для Windows, написанная на Go, которая отслеживает критические S.M.A.R.T. атрибуты жестких дисков (HDD) и твердотельных накопителей (SSD). При обнаружении или изменении состояния проблемных атрибутов, служба отправляет уведомление в Telegram.

## Ключевые возможности

*   **Интеллектуальный мониторинг:** Служба отслеживает состояние дисков и отправляет уведомление только тогда, когда статус проблемы **изменяется** (появляется новая, ухудшается старая или проблема исчезает). Это позволяет избежать спама одинаковыми сообщениями.
*   **Надежная проверка через PowerShell:** Вместо ограниченной утилиты `wmic`, служба использует PowerShell для получения S.M.A.R.T. атрибутов, которые являются надежными предикторами сбоя как для HDD, так и для SSD:
    *   **`Wear`** (Износ SSD): Отслеживает процент израсходованного ресурса твердотельного накопителя.
    *   `Reallocated Sectors Count` (Переназначенные сектора): Сектора, перемещенные из-за ошибок чтения/записи.
    *   `Current Pending Sector Count` (Нестабильные сектора): "Подозрительные" сектора, ожидающие переназначения.
    *   `Uncorrectable Sector Count` (Неисправимые ошибки): Критические ошибки, которые не удалось исправить.
*   **Уведомления в Telegram:** Мгновенное оповещение о проблемах в удобном и наглядном формате (с использованием Markdown).
*   **Внешний файл конфигурации:** Все настройки (токен бота, ID чата) хранятся в файле `config.json`, что позволяет менять их без пересборки программы.
*   **Автоматическое создание конфига:** При первом запуске служба сама создаст шаблон `config.json`, который останется только заполнить.
*   **Ротация логов:** Лог-файл автоматически архивируется и очищается, чтобы не занимать лишнее место на диске.
*   **Простое управление:** Устанавливается и управляется как стандартная служба Windows через командную строку.

## Требования

*   **Windows:** Служба предназначена для работы только на ОС Windows.
*   **PowerShell:** Должен быть доступен (включен по умолчанию в современных версиях Windows).
*   **Компилятор Go:** Необходим для сборки проекта из исходного кода (рекомендуется версия 1.18+).

## Установка и настройка

### Шаг 1: Скачивание исходного кода

Клонируйте репозиторий или скачайте архив с исходным кодом в удобную для вас папку.

```shell
git clone <URL-вашего-репозитория>
cd <папка-проекта>
```

### Шаг 2: Настройка конфигурации

Это самый важный шаг. Службе нужен Telegram-бот для отправки уведомлений.

1.  **Создайте Telegram-бота:**
    *   Найдите в Telegram бота с именем `BotFather`.
    *   Отправьте ему команду `/newbot`.
    *   Следуйте инструкциям, чтобы дать имя вашему боту.
    *   В конце `BotFather` пришлет вам **токен** — длинную строку символов. **Скопируйте и сохраните его.**

2.  **Узнайте ваш Chat ID:**
    *   Найдите в Telegram бота с именем `userinfobot`.
    *   Отправьте ему команду `/start`.
    *   Он пришлет вам ваш **ID**. **Скопируйте и сохраните его.**

3.  **Создайте файл `config.json`:**
    *   Перейдите в папку с исходным кодом и соберите тестовую версию программы: `go build -o DiskMonitorService.exe main.go`
    *   Запустите ее из командной строки: `DiskMonitorService.exe`
    *   Программа сообщит, что конфиг не найден, и **автоматически создаст** файл `config.json` с таким содержимым:
      ```json
      {
        "telegram_token": "YOUR_TOKEN_HERE",
        "telegram_chat_id": "YOUR_CHAT_ID_HERE"
      }
      ```
    *   Откройте этот файл в текстовом редакторе и вставьте ваш **токен** и **ID чата** вместо `YOUR_..._HERE`. Сохраните файл.

### Шаг 3: Сборка приложения

Откройте командную строку в папке с проектом и выполните команду для сборки финальной версии службы:

```shell
go build -ldflags="-w -s -H=windowsgui" -o DiskMonitorService.exe main.go
```

*   Флаги `-ldflags` оптимизируют размер файла и собирают его как фоновое приложение без консольного окна.

## Использование

После сборки у вас будет файл `DiskMonitorService.exe`. Поместите его вместе с файлом `config.json` в постоянное место, откуда будет работать служба (например, `C:\DiskMonitorService`).

> **Важно:** Все команды по установке, удалению и запуску/остановке службы должны выполняться в командной строке, запущенной **от имени администратора**.

#### Тестирование

Перед установкой службы рекомендуется проверить ее работу. Эта команда выполнит одну проверку и принудительно отправит в Telegram сводку о текущем состоянии дисков.

```shell
DiskMonitorService.exe test
```

#### Установка службы

```shell
DiskMonitorService.exe install
```

#### Запуск и остановка службы

*   **Запуск:**
    ```shell
    net start DiskMonitorService
    ```
*   **Остановка:**
    ```shell
    net stop DiskMonitorService
    ```
*   Также управлять службой можно через стандартную оснастку Windows "Службы" (`services.msc`).

#### Удаление службы

```shell
DiskMonitorService.exe remove
```

## Логирование

Служба ведет лог-файл `DiskMonitorService.log`, который находится в той же папке, что и `.exe` файл.

Логи автоматически ротируются благодаря библиотеке `lumberjack` со следующими настройками:
*   Максимальный размер файла: **10 МБ**
*   Максимальное количество старых архивов: **5**
*   Максимальный срок хранения архива: **30 дней**
*   Старые логи сжимаются в формат **.gz**.