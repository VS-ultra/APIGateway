package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ─────────────────────────────────────────────────────────────
// Модели
// ─────────────────────────────────────────────────────────────

type NewsShortDetailed struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	PubDate     time.Time `json:"pub_date"`
	Link        string    `json:"link"`
}

type NewsFullDetailed struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	Description string    `json:"description"`
	PubDate     time.Time `json:"pub_date"`
	Link        string    `json:"link"`
	Comments    []Comment `json:"comments"`
}

type Comment struct {
	ID        int       `json:"id"`
	NewsID    int       `json:"news_id"`
	ParentID  *int      `json:"parent_id,omitempty"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
	Children  []Comment `json:"children,omitempty"`
}

type CommentRequest struct {
	NewsID   int    `json:"news_id"`
	ParentID *int   `json:"parent_id,omitempty"`
	Text     string `json:"text"`
}

type NewsListResponse struct {
	News       []NewsShortDetailed `json:"news"`
	Pagination Pagination          `json:"pagination"`
}

type Pagination struct {
	Page       int `json:"page"`
	TotalPages int `json:"total_pages"`
	PerPage    int `json:"per_page"`
	Total      int `json:"total"`
}

type RequestResult struct {
	Data interface{}
	Err  error
}

type CensorshipRequest struct {
	Text string `json:"text"`
}

// ─────────────────────────────────────────────────────────────
// Контекстные ключи
// ─────────────────────────────────────────────────────────────

type contextKey string

const (
	contextKeyUsername  contextKey = "username"
	contextKeyRequestID contextKey = "request_id"
)

// ─────────────────────────────────────────────────────────────
// JWT валидация
// ─────────────────────────────────────────────────────────────

var jwtSecret []byte

func validateJWT(tokenString string) (string, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("неожиданный алгоритм: %v", t.Header["alg"])
		}
		return jwtSecret, nil
	})
	if err != nil {
		return "", err
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		subject, _ := claims.GetSubject()
		return subject, nil
	}
	return "", fmt.Errorf("невалидный токен")
}

func extractBearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer ")
	}
	return ""
}

// ─────────────────────────────────────────────────────────────
// Middleware
// ─────────────────────────────────────────────────────────────

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tokenStr := extractBearerToken(r); tokenStr != "" {
			if username, err := validateJWT(tokenStr); err == nil && username != "" {
				ctx := context.WithValue(r.Context(), contextKeyUsername, username)
				r = r.WithContext(ctx)
			}
		}
		next.ServeHTTP(w, r)
	})
}

func requireAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenStr := extractBearerToken(r)
		if tokenStr == "" {
			http.Error(w, "Необходима авторизация", http.StatusUnauthorized)
			return
		}
		username, err := validateJWT(tokenStr)
		if err != nil || username == "" {
			http.Error(w, "Токен недействителен или истёк", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), contextKeyUsername, username)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.URL.Query().Get("request_id")
		if requestID == "" {
			requestID = generateRequestID()
		}
		ctx := context.WithValue(r.Context(), contextKeyRequestID, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		requestID, _ := r.Context().Value(contextKeyRequestID).(string)
		log.Printf("[%s] %s %s %s %d %s",
			start.Format("2006-01-02 15:04:05"),
			getClientIP(r),
			r.Method,
			r.URL.Path,
			rw.statusCode,
			requestID,
		)
	})
}
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := os.Getenv("FRONTEND_URL")
		if origin == "" {
			origin = "http://localhost:5173"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
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

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		log.Fatal("JWT_SECRET не задан — запуск невозможен")
	}
	jwtSecret = []byte(secret)

	mux := http.NewServeMux()

	// ── Публичные маршруты (новости и чтение комментариев) ──────────────────
	mux.Handle("/news/latest", authMiddleware(http.HandlerFunc(latestNewsHandler)))
	mux.Handle("/news/filter", authMiddleware(http.HandlerFunc(filterNewsHandler)))
	mux.Handle("/news/", authMiddleware(http.HandlerFunc(newsDetailHandler)))
	mux.HandleFunc("/comments/", getCommentsHandler)

	// ── Защищённый маршрут — создание комментария ───────────────────────────
	mux.HandleFunc("/comments", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			requireAuthMiddleware(addCommentHandler)(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Прокси к SystemAAA
	// /auth/*, /oauth2/* и /login/oauth2/* пробрасываются в Java-сервис.
	mux.HandleFunc("/auth/", authProxyHandler)
	mux.HandleFunc("/oauth2/", authProxyHandler)
	mux.HandleFunc("/login/oauth2/", authProxyHandler)

	handler := requestIDMiddleware(mux)
	handler = loggingMiddleware(handler)
	handler = corsMiddleware(handler)

	log.Println("API Gateway запущен на порту 8080")
	log.Fatal(http.ListenAndServe(":8080", handler))
}

// Прокси к SystemAAA

func authProxyHandler(w http.ResponseWriter, r *http.Request) {
	targetURL := "http://system-aaa:8080" + r.URL.RequestURI()

	// Читаем тело один раз, чтобы передать в новый запрос
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Ошибка чтения тела запроса", http.StatusInternalServerError)
		return
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		http.Error(w, "Ошибка создания запроса к auth-сервису", http.StatusInternalServerError)
		return
	}

	for key, vals := range r.Header {
		for _, v := range vals {
			proxyReq.Header.Add(key, v)
		}
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(proxyReq)
	if err != nil {
		log.Printf("Ошибка при обращении к system-aaa: %v", err)
		http.Error(w, "Auth-сервис недоступен", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Ошибка чтения ответа auth-сервиса", http.StatusInternalServerError)
		return
	}

	for key, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

// Обработчики новостей

func latestNewsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestID, _ := r.Context().Value(contextKeyRequestID).(string)
	params := url.Values{}
	q := r.URL.Query()
	if page := q.Get("page"); page != "" {
		params.Add("page", page)
	}
	if s := q.Get("s"); s != "" {
		params.Add("s", s)
	}
	params.Add("request_id", requestID)

	resp, err := http.Get("http://news-service:8082/news/latest?" + params.Encode())
	if err != nil {
		http.Error(w, "Не удалось получить новости", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "Ошибка сервиса новостей", resp.StatusCode)
		return
	}

	var newsList NewsListResponse
	if err = json.NewDecoder(resp.Body).Decode(&newsList); err != nil {
		http.Error(w, "Ошибка декодирования новостей", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(newsList)
}

func filterNewsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestID, _ := r.Context().Value(contextKeyRequestID).(string)
	params := url.Values{}
	q := r.URL.Query()
	for _, key := range []string{"page", "q", "s", "date_from", "date_to", "sort_by"} {
		if v := q.Get(key); v != "" {
			params.Add(key, v)
		}
	}
	params.Add("request_id", requestID)

	resp, err := http.Get("http://news-service:8082/news/filter?" + params.Encode())
	if err != nil {
		http.Error(w, "Не удалось получить новости", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "Ошибка сервиса новостей", resp.StatusCode)
		return
	}

	var newsList NewsListResponse
	if err = json.NewDecoder(resp.Body).Decode(&newsList); err != nil {
		http.Error(w, "Ошибка декодирования новостей", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(newsList)
}

func newsDetailHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/news/")
	if idStr == "" {
		http.Error(w, "Требуется ID новости", http.StatusBadRequest)
		return
	}
	newsID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Неверный ID новости", http.StatusBadRequest)
		return
	}

	requestID, _ := r.Context().Value(contextKeyRequestID).(string)

	var wg sync.WaitGroup
	resultChan := make(chan RequestResult, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		newsURL := fmt.Sprintf("http://news-service:8082/news/%d?request_id=%s", newsID, requestID)
		resp, err := http.Get(newsURL)
		if err != nil {
			resultChan <- RequestResult{Err: fmt.Errorf("ошибка получения новости: %v", err)}
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			resultChan <- RequestResult{Err: fmt.Errorf("новость не найдена")}
			return
		}
		if resp.StatusCode != http.StatusOK {
			resultChan <- RequestResult{Err: fmt.Errorf("ошибка сервиса новостей: %d", resp.StatusCode)}
			return
		}
		var news NewsFullDetailed
		if err = json.NewDecoder(resp.Body).Decode(&news); err != nil {
			resultChan <- RequestResult{Err: fmt.Errorf("ошибка декодирования новости: %v", err)}
			return
		}
		resultChan <- RequestResult{Data: news}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		commentsURL := fmt.Sprintf("http://comments-service:8081/comments/%d?request_id=%s", newsID, requestID)
		resp, err := http.Get(commentsURL)
		if err != nil {
			resultChan <- RequestResult{Data: []Comment{}}
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			resultChan <- RequestResult{Data: []Comment{}}
			return
		}
		var comments []Comment
		if err = json.NewDecoder(resp.Body).Decode(&comments); err != nil {
			resultChan <- RequestResult{Data: []Comment{}}
			return
		}
		resultChan <- RequestResult{Data: comments}
	}()

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	var news NewsFullDetailed
	var comments []Comment

	for result := range resultChan {
		if result.Err != nil {
			http.Error(w, result.Err.Error(), http.StatusInternalServerError)
			return
		}
		switch data := result.Data.(type) {
		case NewsFullDetailed:
			news = data
		case []Comment:
			comments = data
		}
	}

	news.Comments = comments
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(news)
}

// ─────────────────────────────────────────────────────────────
// Обработчики комментариев
// ─────────────────────────────────────────────────────────────

func getCommentsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	newsIDStr := strings.TrimPrefix(r.URL.Path, "/comments/")
	if newsIDStr == "" {
		http.Error(w, "Требуется ID новости", http.StatusBadRequest)
		return
	}
	newsID, err := strconv.Atoi(newsIDStr)
	if err != nil {
		http.Error(w, "Неверный ID новости", http.StatusBadRequest)
		return
	}

	requestID, _ := r.Context().Value(contextKeyRequestID).(string)
	commentsURL := fmt.Sprintf("http://comments-service:8081/comments/%d?request_id=%s", newsID, requestID)

	resp, err := http.Get(commentsURL)
	if err != nil {
		http.Error(w, "Не удалось получить комментарии", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "Ошибка сервиса комментариев", resp.StatusCode)
		return
	}

	var comments []Comment
	if err = json.NewDecoder(resp.Body).Decode(&comments); err != nil {
		http.Error(w, "Ошибка декодирования комментариев", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(comments)
}

func addCommentHandler(w http.ResponseWriter, r *http.Request) {
	var commentReq CommentRequest
	if err := json.NewDecoder(r.Body).Decode(&commentReq); err != nil {
		http.Error(w, "Неверный JSON", http.StatusBadRequest)
		return
	}
	if commentReq.NewsID <= 0 {
		http.Error(w, "Требуется ID новости", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(commentReq.Text) == "" {
		http.Error(w, "Требуется текст комментария", http.StatusBadRequest)
		return
	}

	requestID, _ := r.Context().Value(contextKeyRequestID).(string)

	// Проверка цензуры
	censorBody, _ := json.Marshal(CensorshipRequest{Text: commentReq.Text})
	censorURL := fmt.Sprintf("http://censorship-service:8083/censor?request_id=%s", requestID)
	censorReq, err := http.NewRequest(http.MethodPost, censorURL, bytes.NewReader(censorBody))
	if err != nil {
		http.Error(w, "Ошибка создания запроса цензуры", http.StatusInternalServerError)
		return
	}
	censorReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	censorResp, err := client.Do(censorReq)
	if err != nil {
		http.Error(w, "Сервис цензурирования недоступен", http.StatusInternalServerError)
		return
	}
	defer censorResp.Body.Close()

	if censorResp.StatusCode == http.StatusBadRequest {
		http.Error(w, "Комментарий содержит недопустимый контент", http.StatusBadRequest)
		return
	}
	if censorResp.StatusCode != http.StatusOK {
		http.Error(w, "Ошибка сервиса цензурирования", http.StatusInternalServerError)
		return
	}

	// Отправка в comments-service
	commentBody, _ := json.Marshal(commentReq)
	commentsURL := fmt.Sprintf("http://comments-service:8081/comments?request_id=%s", requestID)
	commentHTTPReq, err := http.NewRequest(http.MethodPost, commentsURL, bytes.NewReader(commentBody))
	if err != nil {
		http.Error(w, "Ошибка создания запроса комментария", http.StatusInternalServerError)
		return
	}
	commentHTTPReq.Header.Set("Content-Type", "application/json")

	commentResp, err := client.Do(commentHTTPReq)
	if err != nil {
		http.Error(w, "Не удалось добавить комментарий", http.StatusInternalServerError)
		return
	}
	defer commentResp.Body.Close()

	if commentResp.StatusCode != http.StatusCreated {
		http.Error(w, "Ошибка сервиса комментариев", commentResp.StatusCode)
		return
	}

	var newComment Comment
	if err = json.NewDecoder(commentResp.Body).Decode(&newComment); err != nil {
		http.Error(w, "Ошибка декодирования ответа", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(newComment)
}
