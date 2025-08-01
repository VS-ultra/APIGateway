package main

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// CensorshipRequest структура запроса для цензурирования
type CensorshipRequest struct {
	Text string `json:"text"`
}

// CensorshipResponse структура ответа цензурирования
type CensorshipResponse struct {
	IsApproved bool   `json:"is_approved"`
	Message    string `json:"message,omitempty"`
}

// Список запрещенных слов
var forbiddenWords = []string{
	"qwerty",
	"йцукен",
	"zxvbnm",
}

// Middleware для обработки request_id
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.URL.Query().Get("request_id")
		if requestID == "" {
			requestID = generateRequestID()
		}

		ctx := context.WithValue(r.Context(), "request_id", requestID)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}

// Middleware для логирования запросов
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rw, r)
		requestID, _ := r.Context().Value("request_id").(string)
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
	mux := http.NewServeMux()
	mux.HandleFunc("/censor", censorHandler)
	mux.HandleFunc("/health", healthCheckHandler)
	handler := requestIDMiddleware(mux)
	handler = loggingMiddleware(handler)

	log.Println("Сервис цензурирования запущен на порту 8083")
	log.Fatal(http.ListenAndServe(":8083", handler))
}

// censorHandler обрабатывает запросы на цензурирование
func censorHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestID, _ := r.Context().Value("request_id").(string)
	log.Printf("Запрос на цензурирование, request_id: %s", requestID)

	var req CensorshipRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Text) == "" {
		http.Error(w, "Text is required", http.StatusBadRequest)
		return
	}

	// Проверяем текст на наличие запрещенных слов
	isApproved := checkText(req.Text)

	if isApproved {
		log.Printf("Комментарий одобрен: %s, request_id: %s", req.Text, requestID)
		response := CensorshipResponse{
			IsApproved: true,
			Message:    "Comment approved",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	} else {
		log.Printf("Комментарий отклонен: %s, request_id: %s", req.Text, requestID)
		response := CensorshipResponse{
			IsApproved: false,
			Message:    "Comment contains inappropriate content",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
	}
}

// checkText проверяет текст на наличие запрещенных слов
func checkText(text string) bool {
	textLower := strings.ToLower(text)

	for _, word := range forbiddenWords {
		if strings.Contains(textLower, strings.ToLower(word)) {
			return false
		}
	}

	return true
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
		"service":   "censorship-service",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}
