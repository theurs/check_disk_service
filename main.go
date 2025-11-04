package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	serviceName      = "DiskMonitorService"
	logFileName      = "DiskMonitorService.log"
	configFileName   = "config.json"
	telegramMsgLimit = 4096

	CheckStatusOK          = 0 // Все в порядке
	CheckStatusWmicError   = 1 // Ошибка выполнения команды (временная)
	CheckStatusDiskFailure = 2 // Обнаружена проблема с диском (фатальная)

	maxRetries        = 4               // Количество повторных попыток (всего 1+4=5 попыток)
	initialRetryDelay = 5 * time.Second // Начальная задержка перед повтором
)

// Структура для хранения настроек из config.json
type Config struct {
	TelegramToken  string `json:"telegram_token"`
	TelegramChatID string `json:"telegram_chat_id"`
}

var (
	AppConfig       Config // Глобальная переменная для хранения загруженных настроек
	telegramApiBase string // URL для API теперь тоже глобальная переменная
	// Ключ - идентификатор диска, значение - строка с ошибкой
	lastErrorState = make(map[string]string)
)

// Загрузка конфигурации из файла config.json
func loadConfig() {
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %v", err)
	}
	exeDir := filepath.Dir(exePath)
	configFilePath := filepath.Join(exeDir, configFileName)

	// Проверяем, существует ли файл
	if _, err := os.Stat(configFilePath); os.IsNotExist(err) {
		// Файла нет, создаем шаблон
		fmt.Printf("Config file not found. Creating a template at %s\n", configFilePath)
		log.Printf("Config file not found. Creating a template at %s", configFilePath)

		defaultConfig := Config{
			TelegramToken:  "YOUR_TOKEN_HERE",
			TelegramChatID: "YOUR_CHAT_ID_HERE",
		}
		configData, _ := json.MarshalIndent(defaultConfig, "", "  ")

		if err := os.WriteFile(configFilePath, configData, 0666); err != nil {
			log.Fatalf("Failed to write config file template: %v", err)
		}

		log.Fatal("Please edit the config.json file and restart the application.")
		os.Exit(1)
	}

	// Файл есть, читаем его
	file, err := os.ReadFile(configFilePath)
	if err != nil {
		log.Fatalf("Error reading config file: %v", err)
	}

	if err := json.Unmarshal(file, &AppConfig); err != nil {
		log.Fatalf("Error parsing config file (invalid JSON?): %v", err)
	}

	if AppConfig.TelegramToken == "YOUR_TOKEN_HERE" || AppConfig.TelegramChatID == "YOUR_CHAT_ID_HERE" || AppConfig.TelegramToken == "" {
		log.Fatal("Please fill in your actual token and chat_id in config.json")
		os.Exit(1)
	}

	telegramApiBase = "https://api.telegram.org/bot" + AppConfig.TelegramToken
	log.Println("Configuration loaded successfully.")
}

// setupLogging настраивает логирование с ротацией файлов
func setupLogging() {
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %v", err)
	}
	exeDir := filepath.Dir(exePath)
	logFilePath := filepath.Join(exeDir, logFileName)

	// Настраиваем lumberjack для ротации логов
	log.SetOutput(&lumberjack.Logger{
		Filename:   logFilePath, // Путь к лог-файлу
		MaxSize:    10,          // Максимальный размер файла в мегабайтах (MB)
		MaxBackups: 5,           // Максимальное количество старых файлов для хранения
		MaxAge:     30,          // Максимальное количество дней для хранения старых файлов
		Compress:   true,        // Сжимать старые файлы в .gz
	})

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("--- Application starting (with log rotation) ---")
}

func main() {
	setupLogging()
	loadConfig()

	isService, err := svc.IsWindowsService()
	if err != nil {
		log.Fatalf("failed to determine if we are running as a service: %v", err)
	}

	if isService {
		runService(serviceName)
		return
	}

	if len(os.Args) > 1 {
		cmd := os.Args[1]
		switch cmd {
		case "install":
			err = installService(serviceName, serviceName+" Description")
			if err != nil {
				log.Fatalf("failed to install service: %v", err)
			}
			fmt.Printf("Service %s installed successfully.\n", serviceName)
			log.Printf("Service %s installed successfully.", serviceName)
			return
		case "remove":
			err = removeService(serviceName)
			if err != nil {
				log.Fatalf("failed to remove service: %v", err)
			}
			fmt.Printf("Service %s removed successfully.\n", serviceName)
			log.Printf("Service %s removed successfully.", serviceName)
			return
		case "test":
			fmt.Println("Running a one-time stateful check...")
			log.Println("Manual test run triggered.")

			checkDiskStatusAndNotify()

			var summaryMessage string
			if len(lastErrorState) == 0 {
				summaryMessage = "✅ Test complete. No active problems found."
			} else {
				var problems []string
				for _, problemLine := range lastErrorState {
					problems = append(problems, problemLine)
				}
				summaryMessage = fmt.Sprintf("ℹ️ Test complete. Current active problems:\n\n`%s`", strings.Join(problems, "`\n`"))
			}
			log.Println("Sending test summary notification.")
			sendTelegramNotification(summaryMessage)

			fmt.Println("Test complete. See log for details.")
			return
		default:
			log.Fatalf("unknown command: %s", cmd)
		}
	} else {
		fmt.Printf("Usage:\n")
		fmt.Printf("  %s install   - Installs the service.\n", os.Args[0])
		fmt.Printf("  %s remove    - Removes the service.\n", os.Args[0])
		fmt.Printf("  %s test      - Runs a one-time check and sends a summary notification.\n", os.Args[0])
	}
}

// Service is the main service handler.
type Service struct{}

// Execute is the entry point for the service.
func (s *Service) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	log.Printf("%s starting", serviceName)

	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
	log.Printf("%s started", serviceName)

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				checkDiskStatusAndNotify()
			case <-done:
				return
			}
		}
	}()

	log.Println("Service main loop running.")

	for c := range r {
		switch c.Cmd {
		case svc.Interrogate:
			changes <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			log.Printf("%s stopping due to external command", serviceName)
			close(done)
			changes <- svc.Status{State: svc.StopPending}
			return false, 0
		default:
			log.Printf("unexpected control request #%d", c.Cmd)
		}
	}
	return true, 0
}

// checkDiskStatusAndNotify compares current disk state with the last known state.
func checkDiskStatusAndNotify() {
	// --- НАЧАЛО ИЗМЕНЕНИЙ ---
	// PowerShell скрипт теперь запрашивает MediaType (SSD/HDD) и Wear (износ)
	psCommand := `
		$disks = Get-PhysicalDisk;
		if ($null -eq $disks) { exit 0; }
		foreach ($disk in $disks) {
			try {
				$counters = $disk | Get-StorageReliabilityCounter;
				$deviceId = $disk.DeviceId;
				$model = $disk.Model.Trim();
				$mediaType = $disk.MediaType;
				$wear = $counters.Wear;
				$reallocated = $counters.ReallocatedSectors;
				$pending = $counters.CurrentPendingSectors;
				$uncorrected = $counters.ReadErrorsUncorrected;
				Write-Output "Disk[$deviceId]($model) - MediaType: $mediaType - Wear: $wear - ReallocatedSectors: $reallocated - PendingSectors: $pending - UncorrectedErrors: $uncorrected";
			} catch {
				Write-Output "Could not get counters for a disk. Skipping.";
			}
		}
	`
	// --- КОНЕЦ ИЗМЕНЕНИЙ ---

	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", psCommand)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	output, err := cmd.Output()
	if err != nil {
		currentErrorMsg := fmt.Sprintf("Failed to run PowerShell command: %v", err)
		if lastErrorState["powershell_error"] != currentErrorMsg {
			log.Println(currentErrorMsg)
			sendTelegramNotification("⚠️ " + currentErrorMsg)
			lastErrorState = map[string]string{"powershell_error": currentErrorMsg}
		}
		return
	}

	outputStr := string(output)
	log.Printf("PowerShell check result:\n%s", outputStr)

	currentProblems := make(map[string]string)
	// --- НАЧАЛО ИЗМЕНЕНИЙ ---
	// Регулярное выражение теперь ищет и параметр Wear
	re := regexp.MustCompile(`(ReallocatedSectors|PendingSectors|UncorrectedErrors|Wear):\s*(\d+)`)
	// --- КОНЕЦ ИЗМЕНЕНИЙ ---

	scanner := bufio.NewScanner(strings.NewReader(outputStr))
	for scanner.Scan() {
		line := scanner.Text()
		matches := re.FindAllStringSubmatch(line, -1)
		hasProblem := false
		for _, match := range matches {
			value, _ := strconv.Atoi(match[2])
			if value > 0 {
				hasProblem = true
				break
			}
		}
		if hasProblem {
			diskIdentifier := strings.Split(line, " - ")[0]
			currentProblems[diskIdentifier] = line
		}
	}

	if !reflect.DeepEqual(currentProblems, lastErrorState) {
		log.Println("Disk status has changed. Sending notification.")

		// --- НАЧАЛО ИЗМЕНЕНИЙ ---
		// Формирование красивого сообщения с использованием Markdown
		var messageBuilder strings.Builder
		messageBuilder.WriteString("Disk health status has changed!\n\n")

		// Проблемы, которые появились или усугубились
		for disk, problem := range currentProblems {
			if lastErrorState[disk] != problem {
				messageBuilder.WriteString(fmt.Sprintf("🔴 **Problem Detected/Changed:**\n`%s`\n\n", problem))
			}
		}

		// Проблемы, которые были решены
		for disk := range lastErrorState {
			if _, exists := currentProblems[disk]; !exists {
				// disk здесь это "Disk[id](Model)"
				messageBuilder.WriteString(fmt.Sprintf("🟢 **Problem Resolved:**\n`%s` is now OK.\n\n", disk))
			}
		}
		// --- КОНЕЦ ИЗМЕНЕНИЙ ---

		sendTelegramNotification(messageBuilder.String())
		lastErrorState = currentProblems
	} else {
		log.Println("Disk status unchanged. No notification needed.")
	}
}

// sendTelegramNotification formats and sends a message to Telegram with a retry mechanism.
func sendTelegramNotification(message string) {
	hostname, _ := os.Hostname()
	fullMessage := fmt.Sprintf("🖥️ **Host:** `%s`\n\n%s", hostname, message)

	var err error

	for i := 0; i <= maxRetries; i++ {
		if len(fullMessage) > telegramMsgLimit {
			err = sendTelegramDocument(fullMessage)
		} else {
			err = sendTelegramText(fullMessage, false)
		}

		if err == nil {
			log.Println("Telegram notification sent successfully.")
			return
		}

		log.Printf("Failed to send notification (attempt %d/%d): %v", i+1, maxRetries+1, err)

		if i == maxRetries {
			break
		}

		delay := initialRetryDelay * time.Duration(math.Pow(3, float64(i)))
		log.Printf("Waiting for %v before retrying...", delay)
		time.Sleep(delay)
	}

	log.Printf("Gave up sending notification after %d attempts.", maxRetries+1)
}

// sendTelegramText sends a short message.
func sendTelegramText(message string, silent bool) error {
	apiURL := fmt.Sprintf("%s/sendMessage", telegramApiBase)
	params := url.Values{}
	params.Add("chat_id", AppConfig.TelegramChatID)
	params.Add("text", message)
	params.Add("parse_mode", "Markdown")
	if silent {
		params.Add("disable_notification", "true")
	}

	resp, err := http.PostForm(apiURL, params)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %s: %s", resp.Status, string(body))
	}
	return nil
}

// sendTelegramDocument sends a long message as a text file.
func sendTelegramDocument(content string) error {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	w.WriteField("chat_id", AppConfig.TelegramChatID)

	now := time.Now().Format("2006-01-02_15-04-05")
	hostname, _ := os.Hostname()
	fileName := fmt.Sprintf("log_%s_%s.txt", hostname, now)

	fw, err := w.CreateFormFile("document", fileName)
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		return fmt.Errorf("failed to write content to form file: %w", err)
	}
	w.Close()

	apiURL := fmt.Sprintf("%s/sendDocument", telegramApiBase)
	req, err := http.NewRequest("POST", apiURL, &b)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %s: %s", resp.Status, string(body))
	}
	return nil
}

// installService installs the service.
func installService(name, desc string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %s already exists", name)
	}

	exepath, err := os.Executable()
	if err != nil {
		return err
	}

	s, err = m.CreateService(name, exepath, mgr.Config{
		DisplayName: name,
		Description: desc,
		StartType:   mgr.StartAutomatic,
	})
	if err != nil {
		return err
	}
	defer s.Close()

	return nil
}

// removeService removes the service.
func removeService(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("service %s is not installed", name)
	}
	defer s.Close()

	err = s.Delete()
	if err != nil {
		return err
	}

	return nil
}

// runService executes the service handler.
func runService(name string) {
	log.Printf("Service %s starting to run...", name)
	err := svc.Run(name, &Service{})
	if err != nil {
		log.Printf("Service %s failed: %v", name, err)
		return
	}
	log.Printf("Service %s stopped.", name)
}
