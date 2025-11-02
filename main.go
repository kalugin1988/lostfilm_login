package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Конфигурация сервиса
type Config struct {
	CodePhrase       string
	LostfilmURL      string
	LFSession        string
	LFUDV            string
	TelegramBotToken string
	TelegramChatID   string
	CheckInterval    time.Duration
	ServerPort       string
}

// Ответ API с куками
type CookieResponse struct {
	LOSTFILM_URL string `json:"LOSTFILM_URL"`
	LF_SESSION   string `json:"LF_SESSION"`
	LF_UDV       string `json:"LF_UDV"`
	Status       string `json:"status"`
	LastCheck    string `json:"last_check"`
	Message      string `json:"message,omitempty"`
}

// Статус сервиса
type StatusResponse struct {
	Status    string `json:"status"`
	LastCheck string `json:"last_check"`
	NextCheck string `json:"next_check,omitempty"`
}

// Telegram сообщение
type TelegramMessage struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

// Запрос на отправку уведомления о сериале
type SeriesNotificationRequest struct {
	SeriesName       string `json:"series_name"`
	SeriesID         string `json:"series_id"`
	Season           string `json:"season"`
	Episode          string `json:"episode,omitempty"`
	PosterURL        string `json:"poster_url,omitempty"`
	CustomMessage    string `json:"custom_message,omitempty"`
	NotificationType string `json:"notification_type,omitempty"` // new_episode, new_season, info, etc.
}

// Ответ на отправку уведомления
type NotificationResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	SentAt  string `json:"sent_at,omitempty"`
}

// Глобальные переменные
var (
	config    *Config
	configMu  sync.RWMutex
	lastCheck time.Time
	status    string
	statusMu  sync.RWMutex
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("🚀 Запуск сервиса мониторинга LostFilm куков...")

	// Инициализация конфигурации
	if err := initConfig(); err != nil {
		log.Fatalf("❌ Ошибка инициализации конфигурации: %v", err)
	}

	log.Println("✅ Конфигурация загружена успешно")
	log.Printf("📡 URL LostFilm: %s", config.LostfilmURL)
	log.Printf("⏱️ Интервал проверки: %v", config.CheckInterval)
	log.Printf("🔐 Порт сервера: %s", config.ServerPort)

	// Запуск фоновой проверки куков
	go startBackgroundChecker()

	// Настройка HTTP роутинга
	setupRoutes()

	// Запуск HTTP сервера
	startServer()
}

// Инициализация конфигурации из переменных окружения
func initConfig() error {
	// Сначала пробуем загрузить из .env файла
	loadEnvFile()

	intervalStr := getEnv("CHECK_INTERVAL", "5m")
	checkInterval, err := time.ParseDuration(intervalStr)
	if err != nil {
		return fmt.Errorf("неверный формат CHECK_INTERVAL: %v", err)
	}

	// Генерация кодовой фразы по умолчанию если не установлена
	codePhrase := getEnv("CODE_PHRASE", "")
	if codePhrase == "" {
		codePhrase = generateDefaultCode()
		log.Printf("⚠️ CODE_PHRASE не установлен, используем сгенерированный код: %s", codePhrase)
		log.Println("⚠️ Для безопасности установите CODE_PHRASE в .env файле")
	}

	config = &Config{
		CodePhrase:       codePhrase,
		LostfilmURL:      getEnv("LOSTFILM_URL", "https://lostfilm.one"),
		LFSession:        getEnv("LF_SESSION", ""),
		LFUDV:            getEnv("LF_UDV", ""),
		TelegramBotToken: getEnv("TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID:   getEnv("TELEGRAM_CHAT_ID", ""),
		CheckInterval:    checkInterval,
		ServerPort:       getEnv("PORT", "8080"),
	}

	// Валидация обязательных полей
	if config.LFSession == "" {
		return fmt.Errorf("LF_SESSION обязателен для доступа к LostFilm")
	}
	if config.LFUDV == "" {
		return fmt.Errorf("LF_UDV обязателен для доступа к LostFilm")
	}

	if config.TelegramBotToken == "" || config.TelegramChatID == "" {
		log.Println("⚠️ Telegram уведомления отключены (не указан токен или chat ID)")
	}

	return nil
}

// Загрузка переменных из .env файла
func loadEnvFile() {
	envFiles := []string{".env", "env", ".env.local", "config.env"}

	for _, envFile := range envFiles {
		if loadSingleEnvFile(envFile) {
			log.Printf("✅ Загружены переменные из файла: %s", envFile)
			return
		}
	}

	log.Println("⚠️ Файл .env не найден, используем переменные окружения системы")
	createExampleEnvFile()
}

// Загрузка одного .env файла
func loadSingleEnvFile(filename string) bool {
	file, err := os.Open(filename)
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Пропускаем комментарии и пустые строки
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Разбираем ключ=значение
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])

			// Устанавливаем в переменные окружения если еще не установлено
			if os.Getenv(key) == "" {
				os.Setenv(key, value)
			}
		}
	}

	return true
}

// Генерация кодовой фразы по умолчанию
func generateDefaultCode() string {
	return fmt.Sprintf("lostfilm_%d", time.Now().Unix())
}

// Создание примера .env файла
func createExampleEnvFile() {
	exampleEnv := `# Кодовая фраза для доступа к API (обязательно)
CODE_PHRASE=your_secret_password_123

# Настройки LostFilm (обязательно)
LOSTFILM_URL=https://lostfilm.one
LF_SESSION=6b67ce55be09f0c8bfecd68c08ad5410.3682651
LF_UDV=876947b478cd13a33a6ea37b02f1322c

# Настройки Telegram для уведомлений (опционально)
TELEGRAM_BOT_TOKEN=1234567890:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghi
TELEGRAM_CHAT_ID=-1001234567890

# Интервал проверки (по умолчанию 5 минут)
CHECK_INTERVAL=5m

# Порт сервера (по умолчанию 8080)
PORT=8080
`

	if err := os.WriteFile(".env.example", []byte(exampleEnv), 0644); err != nil {
		log.Printf("⚠️ Не удалось создать .env.example: %v", err)
	} else {
		log.Println("📁 Создан файл .env.example - скопируйте его в .env и настройте")
	}
}

// Получение переменной окружения
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Настройка HTTP роутинга
func setupRoutes() {
	http.HandleFunc("/api/cookies", authMiddleware(handleCookies))
	http.HandleFunc("/api/status", authMiddleware(handleStatus))
	http.HandleFunc("/api/telegram/notification", authMiddleware(handleTelegramNotification))
	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/", handleRoot)
}

// Запуск HTTP сервера
func startServer() {
	addr := ":" + config.ServerPort
	log.Printf("🌐 HTTP сервер запускается на %s", addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("❌ Ошибка запуска сервера: %v", err)
	}
}

// Middleware для проверки авторизации
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		configMu.RLock()
		expectedCode := config.CodePhrase
		configMu.RUnlock()

		// Получение кода из заголовка или query параметра
		authCode := r.Header.Get("Authorization")
		if authCode == "" {
			authCode = r.URL.Query().Get("code")
		}

		if authCode != expectedCode {
			log.Printf("🚫 Неавторизованный доступ от %s", r.RemoteAddr)
			sendJSONError(w, "Неверный код авторизации", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

// Обработчик для получения куков
func handleCookies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendJSONError(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	configMu.RLock()
	statusMu.RLock()

	response := CookieResponse{
		LOSTFILM_URL: config.LostfilmURL,
		LF_SESSION:   config.LFSession,
		LF_UDV:       config.LFUDV,
		Status:       status,
		LastCheck:    lastCheck.Format(time.RFC3339),
	}

	statusMu.RUnlock()
	configMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("❌ Ошибка кодирования JSON: %v", err)
		sendJSONError(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	log.Printf("📤 Куки отправлены для %s", r.RemoteAddr)
}

// Обработчик статуса сервиса
func handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendJSONError(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	statusMu.RLock()
	nextCheck := lastCheck.Add(config.CheckInterval)
	response := StatusResponse{
		Status:    status,
		LastCheck: lastCheck.Format(time.RFC3339),
		NextCheck: nextCheck.Format(time.RFC3339),
	}
	statusMu.RUnlock()

	sendJSONResponse(w, response)
}

// Обработчик для отправки уведомлений в Telegram
func handleTelegramNotification(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendJSONError(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	// Проверяем наличие Telegram конфигурации
	configMu.RLock()
	botToken := config.TelegramBotToken
	chatID := config.TelegramChatID
	configMu.RUnlock()

	if botToken == "" || chatID == "" {
		sendJSONError(w, "Telegram не настроен (отсутствует токен или chat ID)", http.StatusBadRequest)
		return
	}

	// Читаем и парсим JSON запрос
	var notificationReq SeriesNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&notificationReq); err != nil {
		sendJSONError(w, "Неверный формат JSON", http.StatusBadRequest)
		return
	}

	// Валидация обязательных полей
	if notificationReq.SeriesName == "" {
		sendJSONError(w, "Обязательное поле: series_name", http.StatusBadRequest)
		return
	}
	if notificationReq.Season == "" {
		sendJSONError(w, "Обязательное поле: season", http.StatusBadRequest)
		return
	}

	// Формируем сообщение для Telegram
	message := formatTelegramMessage(notificationReq)

	// Отправляем сообщение
	success := sendTelegramMessage(botToken, chatID, message, "HTML")

	// Формируем ответ
	response := NotificationResponse{
		Success: success,
		SentAt:  time.Now().Format(time.RFC3339),
	}

	if success {
		response.Message = "Уведомление успешно отправлено в Telegram"
		log.Printf("📢 Отправлено уведомление о сериале: %s", notificationReq.SeriesName)
	} else {
		response.Message = "Ошибка отправки уведомления в Telegram"
		log.Printf("❌ Ошибка отправки уведомления о сериале: %s", notificationReq.SeriesName)
	}

	sendJSONResponse(w, response)
}

// Форматирование сообщения для Telegram
func formatTelegramMessage(req SeriesNotificationRequest) string {
	var message strings.Builder

	// Заголовок в зависимости от типа уведомления
	switch req.NotificationType {
	case "new_episode":
		message.WriteString("🎬 <b>Новая серия!</b>\n\n")
	case "new_season":
		message.WriteString("🌟 <b>Новый сезон!</b>\n\n")
	default:
		message.WriteString("📺 <b>Уведомление о сериале</b>\n\n")
	}

	// Информация о сериале
	message.WriteString(fmt.Sprintf("<b>Сериал:</b> %s\n", escapeHTML(req.SeriesName)))

	if req.SeriesID != "" {
		message.WriteString(fmt.Sprintf("<b>ID:</b> %s\n", req.SeriesID))
	}

	message.WriteString(fmt.Sprintf("<b>Сезон:</b> %s\n", req.Season))

	if req.Episode != "" {
		message.WriteString(fmt.Sprintf("<b>Серия:</b> %s\n", req.Episode))
	}

	// Кастомное сообщение
	if req.CustomMessage != "" {
		message.WriteString(fmt.Sprintf("\n💬 <i>%s</i>\n", escapeHTML(req.CustomMessage)))
	}

	// Постер
	if req.PosterURL != "" {
		message.WriteString(fmt.Sprintf("\n🖼️ <a href=\"%s\">Постер</a>", req.PosterURL))
	}

	// Время отправки
	message.WriteString(fmt.Sprintf("\n\n⏰ <code>%s</code>", time.Now().Format("2006-01-02 15:04:05")))

	return message.String()
}

// Экранирование HTML символов для Telegram
func escapeHTML(text string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(text)
}

// Отправка сообщения в Telegram
func sendTelegramMessage(botToken, chatID, message, parseMode string) bool {
	telegramMsg := TelegramMessage{
		ChatID:    chatID,
		Text:      message,
		ParseMode: parseMode,
	}

	jsonData, err := json.Marshal(telegramMsg)
	if err != nil {
		log.Printf("❌ Ошибка формирования Telegram сообщения: %v", err)
		return false
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("❌ Ошибка отправки в Telegram: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return true
	} else {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("❌ Ошибка API Telegram: %d - %s", resp.StatusCode, string(body))
		return false
	}
}

// Health check endpoint
func handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendJSONError(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	response := map[string]string{
		"status":    "healthy",
		"service":   "lostfilm-cookie-monitor",
		"timestamp": time.Now().Format(time.RFC3339),
	}

	sendJSONResponse(w, response)
}

// Корневой endpoint
func handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendJSONError(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	configMu.RLock()
	codePhrase := config.CodePhrase
	configMu.RUnlock()

	response := map[string]interface{}{
		"service":   "LostFilm Cookie Monitor",
		"version":   "1.0.0",
		"timestamp": time.Now().Format(time.RFC3339),
		"your_code": codePhrase,
		"endpoints": map[string]string{
			"get_cookies":       "/api/cookies?code=YOUR_CODE",
			"get_status":        "/api/status?code=YOUR_CODE",
			"send_notification": "/api/telegram/notification?code=YOUR_CODE",
			"health":            "/health",
		},
	}

	sendJSONResponse(w, response)
}

// Запуск фоновой проверки куков
func startBackgroundChecker() {
	// Первая проверка при запуске
	performCookieCheck()

	// Периодическая проверка
	ticker := time.NewTicker(config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			performCookieCheck()
		}
	}
}

// Выполнение проверки куков
func performCookieCheck() {
	log.Println("🔍 Начало проверки валидности куков...")

	configMu.RLock()
	url := config.LostfilmURL
	session := config.LFSession
	udv := config.LFUDV
	botToken := config.TelegramBotToken
	chatID := config.TelegramChatID
	configMu.RUnlock()

	// Создание HTTP клиента
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Создание запроса
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		handleCheckError("Создание запроса", err, botToken, chatID)
		return
	}

	// Установка кук и заголовков
	req.AddCookie(&http.Cookie{Name: "lf_session", Value: session})
	req.AddCookie(&http.Cookie{Name: "lf_udv", Value: udv})
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	// Выполнение запроса
	resp, err := client.Do(req)
	if err != nil {
		handleCheckError("Выполнение запроса", err, botToken, chatID)
		return
	}
	defer resp.Body.Close()

	// Проверка ответа
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("⚠️ Ошибка чтения тела ответа: %v", err)
	}

	if resp.StatusCode == http.StatusOK && areCookiesValid(body) {
		handleCheckSuccess()
	} else {
		handleCheckFailure(resp.StatusCode, body, botToken, chatID)
	}
}

// Проверка валидности куков по содержимому страницы
func areCookiesValid(body []byte) bool {
	// Ищем элемент с именем пользователя в формате <div class="name">username</div>
	// Это указывает на успешную авторизацию
	userNamePatterns := []string{
		`<div class="name">`,
		`<div class="name" >`,
		`<div class='name'>`,
		`<div class="user-name">`,
		`<span class="name">`,
	}

	bodyStr := string(body)

	// Проверяем наличие любого из паттернов имени пользователя
	for _, pattern := range userNamePatterns {
		if strings.Contains(bodyStr, pattern) {
			log.Printf("✅ Найден элемент имени пользователя: %s", pattern)

			// Дополнительно пытаемся извлечь имя пользователя для логов
			if startIdx := strings.Index(bodyStr, pattern); startIdx != -1 {
				startIdx += len(pattern)
				if endIdx := strings.Index(bodyStr[startIdx:], "<"); endIdx != -1 {
					username := strings.TrimSpace(bodyStr[startIdx : startIdx+endIdx])
					if username != "" {
						log.Printf("👤 Авторизованный пользователь: %s", username)
					}
				}
			}
			return true
		}
	}

	// Дополнительные проверки на признаки авторизации
	authIndicators := []string{
		"my.php",         // Ссылка на личный кабинет
		"logout",         // Кнопка выхода
		"Выйти",          // Кнопка выхода (рус)
		"Личный кабинет", // Личный кабинет
		"Избранное",      // Избранное
	}

	// Проверяем дополнительные индикаторы авторизации
	authCount := 0
	for _, indicator := range authIndicators {
		if strings.Contains(bodyStr, indicator) {
			authCount++
			log.Printf("✅ Найден индикатор авторизации: %s", indicator)
		}
	}

	// Если найдено достаточно индикаторов авторизации, считаем куки валидными
	if authCount >= 2 {
		log.Printf("✅ Найдено %d индикаторов авторизации", authCount)
		return true
	}

	log.Println("❌ Элемент имени пользователя не найден, куки невалидны")
	return false
}

// Обработка успешной проверки
func handleCheckSuccess() {
	log.Println("✅ Куки валидны - пользователь авторизован")
	updateStatus("valid", "Куки активны, пользователь авторизован")
}

// Обработка неудачной проверки
func handleCheckFailure(statusCode int, body []byte, botToken, chatID string) {
	errorMsg := fmt.Sprintf("HTTP %d: Куки невалидны - пользователь не авторизован", statusCode)
	log.Printf("❌ %s", errorMsg)

	updateStatus("invalid", errorMsg)

	// Отправка уведомления в Telegram
	message := fmt.Sprintf("🚨 LostFilm Monitor\n❌ Проблема с авторизацией!\n📊 HTTP статус: %d\n🔐 Куки невалидны\n⏰ Время: %s",
		statusCode, time.Now().Format("2006-01-02 15:04:05"))

	sendTelegramAlert(botToken, chatID, message)
}

// Обработка ошибки проверки
func handleCheckError(operation string, err error, botToken, chatID string) {
	errorMsg := fmt.Sprintf("%s: %v", operation, err)
	log.Printf("❌ %s", errorMsg)

	updateStatus("error", errorMsg)

	message := fmt.Sprintf("🚨 LostFilm Monitor\n⚠️ Ошибка при проверке куков\n🔧 Операция: %s\n💬 Ошибка: %v\n⏰ Время: %s",
		operation, err, time.Now().Format("2006-01-02 15:04:05"))

	sendTelegramAlert(botToken, chatID, message)
}

// Обновление статуса сервиса
func updateStatus(newStatus, message string) {
	statusMu.Lock()
	defer statusMu.Unlock()

	status = newStatus
	lastCheck = time.Now()

	log.Printf("📊 Статус обновлен: %s - %s", newStatus, message)
}

// Отправка уведомления в Telegram (для внутренних нужд)
func sendTelegramAlert(botToken, chatID, message string) {
	if botToken == "" || chatID == "" {
		return
	}

	sendTelegramMessage(botToken, chatID, message, "")
}

// Вспомогательные функции для HTTP ответов

func sendJSONResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("❌ Ошибка кодирования JSON: %v", err)
		http.Error(w, `{"error": "Internal Server Error"}`, http.StatusInternalServerError)
	}
}

func sendJSONError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{
		"error":  message,
		"status": strconv.Itoa(statusCode),
		"time":   time.Now().Format(time.RFC3339),
	})
}
