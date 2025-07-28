package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NewsShortDetailed модель для списка новостей
type NewsShortDetailed struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	PubDate     time.Time `json:"pub_date"`
	Link        string    `json:"link"`
}

// NewsFullDetailed модель для детальной новости
type NewsFullDetailed struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	Description string    `json:"description"`
	PubDate     time.Time `json:"pub_date"`
	Link        string    `json:"link"`
	Comments    []Comment `json:"comments"`
}

// Comment модель комментария
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

// NewsListResponse ответ для списка новостей
type NewsListResponse struct {
	News       []NewsShortDetailed `json:"news"`
	Pagination Pagination          `json:"pagination"`
}

// Pagination структура пагинации
type Pagination struct {
	Page       int `json:"page"`
	TotalPages int `json:"total_pages"`
	PerPage    int `json:"per_page"`
	Total      int `json:"total"`
}

// RequestResult структура для результата запроса
type RequestResult struct {
	Data interface{}
	Err  error
}

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

	// Создаем mux
	mux := http.NewServeMux()

	mux.HandleFunc("/news/latest", latestNewsHandler)
	mux.HandleFunc("/news/filter", filterNewsHandler)
	mux.HandleFunc("/news/", newsDetailHandler)
	mux.HandleFunc("/comments", commentsHandler)
	mux.HandleFunc("/comments/", getCommentsHandler)

	// Применяем middleware
	handler := requestIDMiddleware(mux)
	handler = loggingMiddleware(handler)

	log.Println("API Gateway запущен на порту 8080")
	log.Fatal(http.ListenAndServe(":8080", handler))
}

// commentsHandler обрабатывает запросы к /comments
func commentsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		addCommentHandler(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// getCommentsHandler обрабатывает получение комментариев
func getCommentsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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

	requestID, _ := r.Context().Value("request_id").(string)

	// Проксируем запрос к сервису комментариев
	commentsURL := fmt.Sprintf("http://comments-service:8081/comments/%d?request_id=%s", newsID, requestID)
	commentsResp, err := http.Get(commentsURL)
	if err != nil {
		log.Printf("Ошибка при получении комментариев: %v", err)
		http.Error(w, "Не удалось получить комментарии", http.StatusInternalServerError)
		return
	}
	defer commentsResp.Body.Close()

	if commentsResp.StatusCode == http.StatusOK {
		var comments []Comment
		err = json.NewDecoder(commentsResp.Body).Decode(&comments)
		if err != nil {
			log.Printf("Ошибка декодирования комментариев: %v", err)
			http.Error(w, "Не удалось декодировать комментарии", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(comments)
	} else {
		http.Error(w, "Ошибка сервиса комментариев", commentsResp.StatusCode)
	}
}

func latestNewsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestID, _ := r.Context().Value("request_id").(string)

	// Строим URL для запроса новостей
	queryParams := r.URL.Query()
	newsServiceURL := "http://news-service:8082/news/latest?"

	// Добавляем все параметры
	params := url.Values{}
	if page := queryParams.Get("page"); page != "" {
		params.Add("page", page)
	}
	if search := queryParams.Get("s"); search != "" {
		params.Add("s", search)
	}
	params.Add("request_id", requestID)

	newsServiceURL += params.Encode()

	resp, err := http.Get(newsServiceURL)
	if err != nil {
		log.Printf("Ошибка при получении новостей: %v", err)
		http.Error(w, "Не удалось получить новости", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "Ошибка сервиса новостей", resp.StatusCode)
		return
	}

	var newsList NewsListResponse
	err = json.NewDecoder(resp.Body).Decode(&newsList)
	if err != nil {
		log.Printf("Ошибка декодирования списка новостей: %v", err)
		http.Error(w, "Не удалось декодировать список новостей", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(newsList)
}

func filterNewsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestID, _ := r.Context().Value("request_id").(string)
	queryParams := r.URL.Query()

	newsServiceURL := "http://news-service:8082/news/filter?"

	// Добавляем все параметры
	params := url.Values{}
	if page := queryParams.Get("page"); page != "" {
		params.Add("page", page)
	}
	if q := queryParams.Get("q"); q != "" {
		params.Add("q", q)
	}
	if s := queryParams.Get("s"); s != "" {
		params.Add("s", s)
	}
	if dateFrom := queryParams.Get("date_from"); dateFrom != "" {
		params.Add("date_from", dateFrom)
	}
	if dateTo := queryParams.Get("date_to"); dateTo != "" {
		params.Add("date_to", dateTo)
	}
	if sortBy := queryParams.Get("sort_by"); sortBy != "" {
		params.Add("sort_by", sortBy)
	}
	params.Add("request_id", requestID)

	newsServiceURL += params.Encode()

	resp, err := http.Get(newsServiceURL)
	if err != nil {
		log.Printf("Ошибка при получении отфильтрованных новостей: %v", err)
		http.Error(w, "Не удалось получить новости", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "Ошибка сервиса новостей", resp.StatusCode)
		return
	}

	var newsList NewsListResponse
	err = json.NewDecoder(resp.Body).Decode(&newsList)
	if err != nil {
		log.Printf("Ошибка декодирования списка новостей: %v", err)
		http.Error(w, "Не удалось декодировать список новостей", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(newsList)
}

func newsDetailHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Path
	if len(path) <= len("/news/") {
		http.Error(w, "Требуется ID новости", http.StatusBadRequest)
		return
	}

	idStr := strings.TrimPrefix(path, "/news/")
	newsID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Неверный ID новости", http.StatusBadRequest)
		return
	}

	requestID, _ := r.Context().Value("request_id").(string)

	// Асинхронное выполнение запросов
	var wg sync.WaitGroup
	resultChan := make(chan RequestResult, 2)

	// Запрос новости
	wg.Add(1)
	go func() {
		defer wg.Done()

		newsURL := fmt.Sprintf("http://news-service:8082/news/%d?request_id=%s", newsID, requestID)
		newsResp, err := http.Get(newsURL)
		if err != nil {
			resultChan <- RequestResult{Data: nil, Err: fmt.Errorf("ошибка при получении новости: %v", err)}
			return
		}
		defer newsResp.Body.Close()

		if newsResp.StatusCode != http.StatusOK {
			if newsResp.StatusCode == http.StatusNotFound {
				resultChan <- RequestResult{Data: nil, Err: fmt.Errorf("новость не найдена")}
				return
			}
			resultChan <- RequestResult{Data: nil, Err: fmt.Errorf("ошибка сервиса новостей: %d", newsResp.StatusCode)}
			return
		}

		var news NewsFullDetailed
		err = json.NewDecoder(newsResp.Body).Decode(&news)
		if err != nil {
			resultChan <- RequestResult{Data: nil, Err: fmt.Errorf("ошибка декодирования новости: %v", err)}
			return
		}

		resultChan <- RequestResult{Data: news, Err: nil}
	}()

	// Запрос комментариев
	wg.Add(1)
	go func() {
		defer wg.Done()

		commentsURL := fmt.Sprintf("http://comments-service:8081/comments/%d?request_id=%s", newsID, requestID)
		commentsResp, err := http.Get(commentsURL)
		if err != nil {
			log.Printf("Ошибка при получении комментариев: %v", err)
			resultChan <- RequestResult{Data: []Comment{}, Err: nil}
			return
		}
		defer commentsResp.Body.Close()

		if commentsResp.StatusCode == http.StatusOK {
			var comments []Comment
			err = json.NewDecoder(commentsResp.Body).Decode(&comments)
			if err != nil {
				log.Printf("Ошибка декодирования комментариев: %v", err)
				resultChan <- RequestResult{Data: []Comment{}, Err: nil}
				return
			}
			resultChan <- RequestResult{Data: comments, Err: nil}
		} else {
			resultChan <- RequestResult{Data: []Comment{}, Err: nil}
		}
	}()

	// Ждем завершения всех горутин
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Обрабатываем результаты
	var news NewsFullDetailed
	var comments []Comment
	var hasError bool

	for result := range resultChan {
		if result.Err != nil {
			log.Printf("Ошибка в запросе: %v", result.Err)
			http.Error(w, result.Err.Error(), http.StatusInternalServerError)
			hasError = true
			break
		}

		switch data := result.Data.(type) {
		case NewsFullDetailed:
			news = data
		case []Comment:
			comments = data
		}
	}

	if hasError {
		return
	}

	// Объединяем результаты
	news.Comments = comments

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(news)
}

func addCommentHandler(w http.ResponseWriter, r *http.Request) {
	var commentReq CommentRequest
	err := json.NewDecoder(r.Body).Decode(&commentReq)
	if err != nil {
		http.Error(w, "Неверный JSON", http.StatusBadRequest)
		return
	}

	if commentReq.NewsID <= 0 {
		http.Error(w, "Требуется ID новости", http.StatusBadRequest)
		return
	}
	if commentReq.Text == "" {
		http.Error(w, "Требуется текст комментария", http.StatusBadRequest)
		return
	}

	requestID, _ := r.Context().Value("request_id").(string)

	body, err := json.Marshal(commentReq)
	if err != nil {
		http.Error(w, "Не удалось закодировать комментарий", http.StatusInternalServerError)
		return
	}

	commentsURL := fmt.Sprintf("http://comments-service:8081/comments?request_id=%s", requestID)
	req, err := http.NewRequest("POST", commentsURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "Не удалось создать запрос", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Не удалось добавить комментарий", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		http.Error(w, "Ошибка сервиса комментариев", resp.StatusCode)
		return
	}

	var newComment Comment
	err = json.NewDecoder(resp.Body).Decode(&newComment)
	if err != nil {
		http.Error(w, "Не удалось декодировать комментарий", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(newComment)
}
