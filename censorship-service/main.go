package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"
)

// ─── МОДЕЛИ ───────────────────────────────────────────────────────────────────

type CensorshipRequest struct {
	Text string `json:"text"`
}

type CensorshipResponse struct {
	IsApproved bool   `json:"is_approved"`
	Message    string `json:"message,omitempty"`
}

// ─── ЗАГРУЗКА СЛОВ ────────────────────────────────────────────────────────────

func loadForbiddenWords(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось загрузить список слов из %s: %w", path, err)
	}

	var words []string
	for _, line := range strings.Split(string(data), "\n") {
		word := strings.TrimSpace(line)
		// пропускаем пустые строки и комментарии (#)
		if word != "" && !strings.HasPrefix(word, "#") {
			words = append(words, word)
		}
	}

	if len(words) == 0 {
		return nil, fmt.Errorf("файл %s пустой или не содержит слов", path)
	}

	return words, nil
}

func checkText(text string, forbiddenWords []string) bool {
	textLower := strings.ToLower(text)
	for _, word := range forbiddenWords {
		if strings.Contains(textLower, strings.ToLower(word)) {
			return false
		}
	}
	return true
}

// HANDLERS

func makeCensorHandler(forbiddenWords []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		requestID, _ := r.Context().Value("request_id").(string)
		log.Printf("[INFO] Запрос на цензурирование, request_id: %s", requestID)

		var req CensorshipRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if strings.TrimSpace(req.Text) == "" {
			http.Error(w, "Text is required", http.StatusBadRequest)
			return
		}

		isApproved := checkText(req.Text, forbiddenWords)

		w.Header().Set("Content-Type", "application/json")

		if isApproved {
			log.Printf("[INFO] Комментарий одобрен, request_id: %s", requestID)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(CensorshipResponse{
				IsApproved: true,
				Message:    "Comment approved",
			})
		} else {
			log.Printf("[INFO] Комментарий отклонён, request_id: %s", requestID)
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(CensorshipResponse{
				IsApproved: false,
				Message:    "Comment contains inappropriate content",
			})
		}
	}
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now(),
		"service":   "censorship-service",
	})
}

// MIDDLEWARE

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.URL.Query().Get("request_id")
		if requestID == "" {
			requestID = generateRequestID()
		}
		ctx := context.WithValue(r.Context(), "request_id", requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		requestID, _ := r.Context().Value("request_id").(string)
		log.Printf("[%s] %s %s %s %d %s",
			start.Format("2006-01-02 15:04:05"),
			getClientIP(r), r.Method, r.URL.Path, rw.statusCode, requestID,
		)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func getClientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
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

	wordsPath := os.Getenv("FORBIDDEN_WORDS_PATH")
	if wordsPath == "" {
		wordsPath = "forbidden_words.txt"
	}

	words, err := loadForbiddenWords(wordsPath)
	if err != nil {
		log.Fatalf("[FATAL] %v", err)
	}
	log.Printf("[INFO] Загружено %d запрещённых слов из %s", len(words), wordsPath)

	mux := http.NewServeMux()
	mux.HandleFunc("/censor", makeCensorHandler(words))
	mux.HandleFunc("/health", healthCheckHandler)

	handler := requestIDMiddleware(mux)
	handler = loggingMiddleware(handler)

	log.Println("[INFO] Сервис цензурирования запущен на порту 8083")
	log.Fatal(http.ListenAndServe(":8083", handler))
}
