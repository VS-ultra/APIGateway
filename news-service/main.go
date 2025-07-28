package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

const PER_PAGE = 15 // Константа для количества элементов на страницу

// config структура для конфигурации из config.json
type config struct {
	RSS           []string `json:"rss"`
	RequestPeriod int      `json:"request_period"`
}

// RSS структура для парсинга RSS-ленты
type RSS struct {
	XMLName xml.Name `xml:"rss"`
	Channel Channel  `xml:"channel"`
}

// Channel содержит список новостей
type Channel struct {
	Items []Item `xml:"item"`
}

// Item представляет одну новость из RSS
type Item struct {
	Title       string `xml:"title"`
	Description string `xml:"description"`
	Link        string `xml:"link"`
	PubDate     string `xml:"pubDate"`
	Content     string `xml:"content"`
}

// News структура новости в базе данных
type News struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	Description string    `json:"description"`
	Link        string    `json:"link"`
	PubDate     time.Time `json:"pub_date"`
	CreatedAt   time.Time `json:"created_at"`
}

// NewsListResponse ответ со списком новостей
type NewsListResponse struct {
	News       []News     `json:"news"`
	Pagination Pagination `json:"pagination"`
}

// Pagination структура пагинации
type Pagination struct {
	Page       int `json:"page"`
	TotalPages int `json:"total_pages"`
	PerPage    int `json:"per_page"`
	Total      int `json:"total"`
}

// Database connection
var db *sql.DB

// Middleware для обработки request_id
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.URL.Query().Get("request_id")
		if requestID == "" {
			requestID = generateRequestID()
		}

		// Добавляем request_id в контекст
		ctx := context.WithValue(r.Context(), "request_id", requestID)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}

// Middleware для логирования запросов
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Создаем ResponseWriter для захвата статус кода
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rw, r)

		// Получаем request_id из контекста
		requestID, _ := r.Context().Value("request_id").(string)

		// Логируем запрос
		log.Printf("[%s] %s %s %s %d %v",
			start.Format("2006-01-02 15:04:05"),
			getClientIP(r),
			r.Method,
			r.URL.Path,
			rw.statusCode,
			requestID,
		)
	})
}

// responseWriter для захвата статус кода
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Получение IP адреса клиента
func getClientIP(r *http.Request) string {
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		return strings.Split(forwarded, ",")[0]
	}
	return r.RemoteAddr
}

// Генерация случайного request_id
func generateRequestID() string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func main() {
	rand.Seed(time.Now().UnixNano())

	// Читаем config.json из корня /app
	b, err := ioutil.ReadFile("./config.json")
	if err != nil {
		log.Fatal("конфиг не найден:", err)
	}
	var cfg config
	if err := json.Unmarshal(b, &cfg); err != nil {
		log.Fatal("не удалось распарсить config.json:", err)
	}

	// Получение переменных окружения для подключения к БД
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dbUser := os.Getenv("DB_USER")
	dbPassword := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")

	// Проверка наличия всех переменных окружения
	if dbHost == "" || dbPort == "" || dbUser == "" || dbPassword == "" || dbName == "" {
		log.Fatal("Необходимо задать все переменные окружения: DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME")
	}

	// Формирование строки подключения
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName)

	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Ошибка подключения к БД:", err)
	}
	defer db.Close()

	// Проверяем соединение
	if err = db.Ping(); err != nil {
		log.Fatal("Не удается подключиться к БД:", err)
	}

	// Запускаем периодическое обновление новостей в отдельной горутине
	go func() {
		ticker := time.NewTicker(time.Duration(cfg.RequestPeriod) * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			updateNewsFromRSS(cfg.RSS)
		}
	}()

	// Загружаем новости при старте
	updateNewsFromRSS(cfg.RSS)

	// Создаем mux
	mux := http.NewServeMux()

	// Настройка маршрутов
	mux.HandleFunc("/news/latest", latestNewsHandler)
	mux.HandleFunc("/news/filter", filterNewsHandler)
	mux.HandleFunc("/news/", newsDetailHandler)
	mux.HandleFunc("/health", healthCheckHandler)

	// Применяем middleware
	handler := requestIDMiddleware(mux)
	handler = loggingMiddleware(handler)

	log.Println("Сервис новостей запущен на порту 8082")
	log.Fatal(http.ListenAndServe(":8082", handler))
}

// updateNewsFromRSS загружает новости из RSS-источников
func updateNewsFromRSS(rssSources []string) {
	log.Println("Начинаем обновление новостей из RSS...")
	totalAdded := 0
	for _, rssURL := range rssSources {
		items, err := fetchRSSFeed(rssURL)
		if err != nil {
			log.Printf("Ошибка загрузки RSS %s: %v", rssURL, err)
			continue
		}
		added := 0
		for _, item := range items {
			if saveNewsItem(item) {
				added++
			}
		}
		totalAdded += added
		log.Printf("Загружено %d новостей из %s", added, rssURL)
	}
	log.Printf("Обновление завершено. Добавлено новостей: %d", totalAdded)
}

// fetchRSSFeed загружает и парсит RSS-ленту
func fetchRSSFeed(rssURL string) ([]Item, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(rssURL)
	if err != nil {
		return nil, fmt.Errorf("ошибка загрузки RSS: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP ошибка: %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %v", err)
	}

	var rss RSS
	err = xml.Unmarshal(body, &rss)
	if err != nil {
		return nil, fmt.Errorf("ошибка парсинга RSS: %v", err)
	}

	return rss.Channel.Items, nil
}

// saveNewsItem сохраняет новость в базу данных
func saveNewsItem(item Item) bool {
	// Парсим дату публикации
	var pubDate time.Time
	if item.PubDate != "" {
		if parsed, err := time.Parse(time.RFC1123, item.PubDate); err == nil {
			pubDate = parsed
		} else if parsed, err := time.Parse(time.RFC1123Z, item.PubDate); err == nil {
			pubDate = parsed
		} else {
			pubDate = time.Now()
		}
	} else {
		pubDate = time.Now()
	}

	// Подготавливаем данные
	title := strings.TrimSpace(item.Title)
	description := strings.TrimSpace(item.Description)
	content := strings.TrimSpace(item.Content)
	link := strings.TrimSpace(item.Link)

	if title == "" || link == "" {
		return false // Пропускаем новости без заголовка или ссылки
	}

	if content == "" {
		content = description // Если контента нет, используем описание
	}

	// Сохраняем в базу данных
	query := `
		INSERT INTO news (title, content, description, link, pub_date)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (link) DO NOTHING
	`
	result, err := db.Exec(query, title, content, description, link, pubDate)
	if err != nil {
		log.Printf("Ошибка сохранения новости '%s': %v", title, err)
		return false
	}

	rowsAffected, _ := result.RowsAffected()
	return rowsAffected > 0
}

// latestNewsHandler возвращает последние новости
func latestNewsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestID, _ := r.Context().Value("request_id").(string)
	log.Printf("Запрос последних новостей, request_id: %s", requestID)

	// Получаем параметры запроса
	pageParam := r.URL.Query().Get("page")
	page := 1
	if pageParam != "" {
		var err error
		page, err = strconv.Atoi(pageParam)
		if err != nil || page < 1 {
			page = 1
		}
	}

	searchQuery := r.URL.Query().Get("s")

	offset := (page - 1) * PER_PAGE

	news, total, err := getLatestNews(searchQuery, PER_PAGE, offset)
	if err != nil {
		log.Printf("Ошибка получения новостей: %v", err)
		http.Error(w, "Failed to get news", http.StatusInternalServerError)
		return
	}

	totalPages := int(math.Ceil(float64(total) / float64(PER_PAGE)))

	response := NewsListResponse{
		News: news,
		Pagination: Pagination{
			Page:       page,
			TotalPages: totalPages,
			PerPage:    PER_PAGE,
			Total:      total,
		},
	}

	log.Printf("Возвращено %d новостей, страница %d из %d, request_id: %s", len(news), page, totalPages, requestID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// filterNewsHandler фильтрует новости по параметрам
func filterNewsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestID, _ := r.Context().Value("request_id").(string)
	log.Printf("Запрос фильтрации новостей, request_id: %s", requestID)

	query := r.URL.Query().Get("q")
	searchQuery := r.URL.Query().Get("s") // Добавляем поддержку параметра s
	dateFrom := r.URL.Query().Get("date_from")
	dateTo := r.URL.Query().Get("date_to")
	sortBy := r.URL.Query().Get("sort_by")

	// Если есть параметр s, используем его для поиска
	if searchQuery != "" && query == "" {
		query = searchQuery
	}

	pageParam := r.URL.Query().Get("page")
	page := 1
	if pageParam != "" {
		var err error
		page, err = strconv.Atoi(pageParam)
		if err != nil || page < 1 {
			page = 1
		}
	}

	offset := (page - 1) * PER_PAGE

	news, total, err := filterNews(query, dateFrom, dateTo, sortBy, PER_PAGE, offset)
	if err != nil {
		log.Printf("Ошибка фильтрации новостей: %v", err)
		http.Error(w, "Failed to filter news", http.StatusInternalServerError)
		return
	}

	totalPages := int(math.Ceil(float64(total) / float64(PER_PAGE)))

	response := NewsListResponse{
		News: news,
		Pagination: Pagination{
			Page:       page,
			TotalPages: totalPages,
			PerPage:    PER_PAGE,
			Total:      total,
		},
	}

	log.Printf("Фильтрация: найдено %d новостей, страница %d из %d, request_id: %s", len(news), page, totalPages, requestID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// newsDetailHandler возвращает детальную информацию о новости
func newsDetailHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestID, _ := r.Context().Value("request_id").(string)

	path := r.URL.Path
	if len(path) < 7 {
		http.Error(w, "News ID required", http.StatusBadRequest)
		return
	}

	idStr := path[6:]
	newsID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid news ID", http.StatusBadRequest)
		return
	}

	log.Printf("Запрос детальной новости ID: %d, request_id: %s", newsID, requestID)

	news, err := getNewsByID(newsID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "News not found", http.StatusNotFound)
			return
		}
		log.Printf("Ошибка получения новости: %v", err)
		http.Error(w, "Failed to get news", http.StatusInternalServerError)
		return
	}

	log.Printf("Найдена новость: %s, request_id: %s", news.Title, requestID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(news)
}

// healthCheckHandler проверка состояния сервиса
func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now(),
		"service":   "news-service",
	}

	if err := db.Ping(); err != nil {
		status["status"] = "error"
		status["database"] = "disconnected"
	} else {
		status["database"] = "connected"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// getLatestNews получает последние новости из БД с поиском
func getLatestNews(searchQuery string, limit, offset int) ([]News, int, error) {
	var countQuery, newsQuery string
	var args []interface{}

	if searchQuery != "" {
		// Поиск по заголовку с использованием ILIKE
		countQuery = "SELECT COUNT(*) FROM news WHERE title ILIKE $1"
		newsQuery = `
			SELECT id, title, content, description, link, pub_date, created_at
			FROM news
			WHERE title ILIKE $1
			ORDER BY pub_date DESC, id DESC
			LIMIT $2 OFFSET $3
		`
		searchPattern := "%" + searchQuery + "%"
		args = []interface{}{searchPattern, limit, offset}
	} else {
		// Все новости
		countQuery = "SELECT COUNT(*) FROM news"
		newsQuery = `
			SELECT id, title, content, description, link, pub_date, created_at
			FROM news
			ORDER BY pub_date DESC, id DESC
			LIMIT $1 OFFSET $2
		`
		args = []interface{}{limit, offset}
	}

	// Получаем общее количество
	var total int
	if searchQuery != "" {
		searchPattern := "%" + searchQuery + "%"
		err := db.QueryRow(countQuery, searchPattern).Scan(&total)
		if err != nil {
			return nil, 0, err
		}
	} else {
		err := db.QueryRow(countQuery).Scan(&total)
		if err != nil {
			return nil, 0, err
		}
	}

	// Получаем новости
	rows, err := db.Query(newsQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var news []News
	for rows.Next() {
		var n News
		err := rows.Scan(&n.ID, &n.Title, &n.Content, &n.Description, &n.Link, &n.PubDate, &n.CreatedAt)
		if err != nil {
			return nil, 0, err
		}
		news = append(news, n)
	}

	return news, total, nil
}

// filterNews фильтрует новости по параметрам
func filterNews(searchQuery, dateFrom, dateTo, sortBy string, limit, offset int) ([]News, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	if searchQuery != "" {
		conditions = append(conditions, fmt.Sprintf("(to_tsvector('russian', title) @@ plainto_tsquery('russian', $%d) OR to_tsvector('russian', content) @@ plainto_tsquery('russian', $%d))", argIndex, argIndex))
		args = append(args, searchQuery)
		argIndex++
	}

	if dateFrom != "" {
		if parsedDate, err := time.Parse("2006-01-02", dateFrom); err == nil {
			conditions = append(conditions, fmt.Sprintf("pub_date >= $%d", argIndex))
			args = append(args, parsedDate)
			argIndex++
		}
	}

	if dateTo != "" {
		if parsedDate, err := time.Parse("2006-01-02", dateTo); err == nil {
			conditions = append(conditions, fmt.Sprintf("pub_date <= $%d", argIndex))
			args = append(args, parsedDate.Add(24*time.Hour-time.Second))
			argIndex++
		}
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	orderClause := "ORDER BY pub_date DESC, id DESC"
	if sortBy == "title" {
		orderClause = "ORDER BY title ASC"
	} else if sortBy == "date_asc" {
		orderClause = "ORDER BY pub_date ASC, id ASC"
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM news %s", whereClause)
	var total int
	err := db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	query := fmt.Sprintf(`
		SELECT id, title, content, description, link, pub_date, created_at
		FROM news
		%s
		%s
		LIMIT $%d OFFSET $%d
	`, whereClause, orderClause, argIndex, argIndex+1)

	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var news []News
	for rows.Next() {
		var n News
		err := rows.Scan(&n.ID, &n.Title, &n.Content, &n.Description, &n.Link, &n.PubDate, &n.CreatedAt)
		if err != nil {
			return nil, 0, err
		}
		news = append(news, n)
	}

	return news, total, nil
}

// getNewsByID получает новость по ID
func getNewsByID(id int) (*News, error) {
	query := `
		SELECT id, title, content, description, link, pub_date, created_at
		FROM news
		WHERE id = $1
	`

	news := &News{}
	err := db.QueryRow(query, id).Scan(
		&news.ID,
		&news.Title,
		&news.Content,
		&news.Description,
		&news.Link,
		&news.PubDate,
		&news.CreatedAt,
	)

	return news, err
}
