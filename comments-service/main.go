package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// Comment структура комментария
type Comment struct {
	ID          int       `json:"id"`
	NewsID      int       `json:"news_id"`
	ParentID    *int      `json:"parent_id,omitempty"`
	Text        string    `json:"text"`
	CreatedAt   time.Time `json:"created_at"`
	IsModerated bool      `json:"is_moderated"`
	IsApproved  bool      `json:"is_approved"`
	Children    []Comment `json:"children,omitempty"`
}

// CommentRequest структура для создания комментария
type CommentRequest struct {
	NewsID   int    `json:"news_id"`
	ParentID *int   `json:"parent_id,omitempty"`
	Text     string `json:"text"`
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

	// Получение переменных окружения
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

	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Ошибка подключения к БД:", err)
	}
	defer db.Close()

	// Проверяем соединение
	if err = db.Ping(); err != nil {
		log.Fatal("Не удается подключиться к БД:", err)
	}

	// Создаем mux
	mux := http.NewServeMux()

	// Настройка маршрутов
	mux.HandleFunc("/comments", commentsHandler)
	mux.HandleFunc("/comments/", getCommentsByNewsHandler)

	// Применяем middleware
	handler := requestIDMiddleware(mux)
	handler = loggingMiddleware(handler)

	log.Println("Сервис комментариев запущен на порту 8081")
	log.Fatal(http.ListenAndServe(":8081", handler))
}

// commentsHandler обрабатывает запросы к /comments
func commentsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		createCommentHandler(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// createCommentHandler создает новый комментарий
func createCommentHandler(w http.ResponseWriter, r *http.Request) {
	requestID, _ := r.Context().Value("request_id").(string)
	log.Printf("Создание комментария, request_id: %s", requestID)

	var commentReq CommentRequest
	err := json.NewDecoder(r.Body).Decode(&commentReq)
	if err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Валидация
	if commentReq.NewsID <= 0 {
		http.Error(w, "News ID is required and must be positive", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(commentReq.Text) == "" {
		http.Error(w, "Comment text is required", http.StatusBadRequest)
		return
	}

	// Проверяем существование родительского комментария если указан
	if commentReq.ParentID != nil {
		var exists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM comments WHERE id = $1)", *commentReq.ParentID).Scan(&exists)
		if err != nil || !exists {
			http.Error(w, "Parent comment not found", http.StatusBadRequest)
			return
		}
	}

	// Сохраняем комментарий в БД (для тестирования автоматически одобряем)
	var commentID int
	query := `
        INSERT INTO comments (news_id, parent_id, text, created_at, is_moderated, is_approved)
        VALUES ($1, $2, $3, $4, $5, $6)
        RETURNING id
    `
	err = db.QueryRow(query, commentReq.NewsID, commentReq.ParentID, commentReq.Text,
		time.Now(), true, true).Scan(&commentID)
	if err != nil {
		log.Printf("Ошибка сохранения комментария: %v", err)
		http.Error(w, "Failed to create comment", http.StatusInternalServerError)
		return
	}

	// Получаем созданный комментарий
	comment, err := getCommentByID(commentID)
	if err != nil {
		log.Printf("Ошибка получения созданного комментария: %v", err)
		http.Error(w, "Comment created but failed to retrieve", http.StatusInternalServerError)
		return
	}

	log.Printf("Создан новый комментарий: ID=%d, NewsID=%d, Text=%s, request_id=%s",
		comment.ID, comment.NewsID, comment.Text, requestID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(comment)
}

// getCommentsByNewsHandler возвращает комментарии для конкретной новости
func getCommentsByNewsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestID, _ := r.Context().Value("request_id").(string)

	path := r.URL.Path
	if len(path) <= len("/comments/") {
		http.Error(w, "News ID required", http.StatusBadRequest)
		return
	}

	newsIDStr := strings.TrimPrefix(path, "/comments/")
	newsID, err := strconv.Atoi(newsIDStr)
	if err != nil {
		http.Error(w, "Invalid news ID", http.StatusBadRequest)
		return
	}

	log.Printf("Получение комментариев для новости ID: %d, request_id: %s", newsID, requestID)

	comments, err := getCommentsByNewsID(newsID)
	if err != nil {
		log.Printf("Ошибка получения комментариев: %v", err)
		http.Error(w, "Failed to get comments", http.StatusInternalServerError)
		return
	}

	commentTree := buildCommentTree(comments)

	log.Printf("Найдено комментариев: %d для новости %d, request_id: %s", len(commentTree), newsID, requestID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(commentTree)
}

// getCommentByID получает комментарий по ID
func getCommentByID(id int) (*Comment, error) {
	query := `
        SELECT id, news_id, parent_id, text, created_at, is_moderated, is_approved
        FROM comments
        WHERE id = $1
    `

	comment := &Comment{}
	err := db.QueryRow(query, id).Scan(
		&comment.ID,
		&comment.NewsID,
		&comment.ParentID,
		&comment.Text,
		&comment.CreatedAt,
		&comment.IsModerated,
		&comment.IsApproved,
	)

	return comment, err
}

// getCommentsByNewsID получает все комментарии для новости
func getCommentsByNewsID(newsID int) ([]Comment, error) {
	query := `
        SELECT id, news_id, parent_id, text, created_at, is_moderated, is_approved
        FROM comments
        WHERE news_id = $1 AND is_approved = true
        ORDER BY created_at ASC
    `

	rows, err := db.Query(query, newsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []Comment
	for rows.Next() {
		var comment Comment
		err := rows.Scan(
			&comment.ID,
			&comment.NewsID,
			&comment.ParentID,
			&comment.Text,
			&comment.CreatedAt,
			&comment.IsModerated,
			&comment.IsApproved,
		)
		if err != nil {
			return nil, err
		}
		comments = append(comments, comment)
	}

	return comments, nil
}

// buildCommentTree строит дерево комментариев
func buildCommentTree(comments []Comment) []Comment {
	commentMap := make(map[int]*Comment)
	var roots []Comment

	// Создаем карту комментариев
	for i := range comments {
		commentMap[comments[i].ID] = &comments[i]
		comments[i].Children = []Comment{} // Инициализируем пустой слайс
	}

	// Строим дерево
	for i := range comments {
		if comments[i].ParentID == nil {
			// Корневой комментарий
			roots = append(roots, comments[i])
		} else {
			// Дочерний комментарий
			parentID := *comments[i].ParentID
			if parent, exists := commentMap[parentID]; exists {
				parent.Children = append(parent.Children, comments[i])
			}
		}
	}

	return roots
}
