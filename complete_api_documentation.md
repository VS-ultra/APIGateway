##  API Gateway (порт 8080) - Основной интерфейс

###  Работа с новостями

#### 1. Получение последних новостей
```bash
# Базовый запрос
curl "http://localhost:8080/news/latest"

# С пагинацией
curl "http://localhost:8080/news/latest?page=1"
curl "http://localhost:8080/news/latest?page=2"

# С поиском по заголовку
curl "http://localhost:8080/news/latest?s=технологии"
curl "http://localhost:8080/news/latest?s=golang"

# Комбинированный запрос
curl "http://localhost:8080/news/latest?page=2&s=программирование"

# С кастомным request_id
curl "http://localhost:8080/news/latest?page=1&request_id=my_custom_id"
```

#### 2. Фильтрация новостей (расширенный поиск)
```bash
# Базовая фильтрация
curl "http://localhost:8080/news/filter"

# Поиск по содержимому (полнотекстовый поиск)
curl "http://localhost:8080/news/filter?q=искусственный%20интеллект"

# Поиск по заголовку (параметр s)
curl "http://localhost:8080/news/filter?s=технологии"

# Фильтр по дате (от)
curl "http://localhost:8080/news/filter?date_from=2025-01-01"

# Фильтр по дате (до)
curl "http://localhost:8080/news/filter?date_to=2025-12-31"

# Диапазон дат
curl "http://localhost:8080/news/filter?date_from=2025-07-01&date_to=2025-07-31"

# Сортировка по дате (по умолчанию - новые сначала)
curl "http://localhost:8080/news/filter?sort_by=date_asc"

# Сортировка по заголовку
curl "http://localhost:8080/news/filter?sort_by=title"

# Комплексный запрос
curl "http://localhost:8080/news/filter?q=python&date_from=2025-07-01&sort_by=title&page=1"

# Все параметры сразу
curl "http://localhost:8080/news/filter?q=разработка&s=golang&date_from=2025-01-01&date_to=2025-12-31&sort_by=date_asc&page=2&request_id=complex_search"
```

#### 3. Получение детальной новости
```bash
# Базовый запрос (асинхронно загружает новость + комментарии)
curl "http://localhost:8080/news/1"
curl "http://localhost:8080/news/999"

# С кастомным request_id
curl "http://localhost:8080/news/1?request_id=detail_view_123"
```

###  Работа с комментариями

#### 4. Создание комментариев
```bash
# Корневой комментарий
curl -X POST "http://localhost:8080/comments" \
  -H "Content-Type: application/json" \
  -d '{"news_id": 1, "text": "Отличная статья!"}'

# Вложенный комментарий (ответ на комментарий)
curl -X POST "http://localhost:8080/comments" \
  -H "Content-Type: application/json" \
  -d '{"news_id": 1, "parent_id": 1, "text": "Согласен с вами!"}'

# Многоуровневая вложенность
curl -X POST "http://localhost:8080/comments" \
  -H "Content-Type: application/json" \
  -d '{"news_id": 1, "parent_id": 2, "text": "А я не согласен"}'

# С кастомным request_id
curl -X POST "http://localhost:8080/comments?request_id=comment_add_123" \
  -H "Content-Type: application/json" \
  -d '{"news_id": 1, "text": "Комментарий с трекингом"}'

```

#### 5. Получение комментариев
```bash
# Все комментарии для новости (иерархическое дерево)
curl "http://localhost:8080/comments/1"
curl "http://localhost:8080/comments/999"

# С кастомным request_id
curl "http://localhost:8080/comments/1?request_id=get_comments_123"
```

##  Прямой доступ к микросервисам

###  Comments Service (порт 8081)

#### 6. Прямое управление комментариями
```bash
# Создание комментария напрямую
curl -X POST "http://localhost:8081/comments" \
  -H "Content-Type: application/json" \
  -d '{"news_id": 1, "text": "Прямой комментарий"}'

# С parent_id
curl -X POST "http://localhost:8081/comments" \
  -H "Content-Type: application/json" \
  -d '{"news_id": 1, "parent_id": 1, "text": "Прямой ответ"}'

# Получение комментариев напрямую
curl "http://localhost:8081/comments/1"

# Проверка здоровья сервиса
curl "http://localhost:8081/health"

# Все с request_id
curl -X POST "http://localhost:8081/comments?request_id=direct_123" \
  -H "Content-Type: application/json" \
  -d '{"news_id": 1, "text": "Прямой комментарий с ID"}'
```

###  News Service (порт 8082)

#### 7. Прямая работа с новостями
```bash
# Последние новости
curl "http://localhost:8082/news/latest"
curl "http://localhost:8082/news/latest?page=1&s=технологии"

# Фильтрация
curl "http://localhost:8082/news/filter?q=python&sort_by=title"
curl "http://localhost:8082/news/filter?date_from=2025-07-01&date_to=2025-07-31"

# Детальная новость
curl "http://localhost:8082/news/1"

# Проверка здоровья
curl "http://localhost:8082/health"

# С request_id
curl "http://localhost:8082/news/latest?request_id=direct_news_123"
```

###  Censorship Service (порт 8083)

#### 8. Проверка цензуры
```bash
# Проверка обычного текста
curl -X POST "http://localhost:8083/censor" \
  -H "Content-Type: application/json" \
  -d '{"text": "Это обычный комментарий"}'

# Проверка запрещенного контента
curl -X POST "http://localhost:8083/censor" \
  -H "Content-Type: application/json" \
  -d '{"text": "Текст содержит qwerty"}'

curl -X POST "http://localhost:8083/censor" \
  -H "Content-Type: application/json" \
  -d '{"text": "Проверяем йцукен в тексте"}'

curl -X POST "http://localhost:8083/censor" \
  -H "Content-Type: application/json" \
  -d '{"text": "А что с zxvbnm?"}'

# Проверка здоровья
curl "http://localhost:8083/health"

# С request_id
curl -X POST "http://localhost:8083/censor?request_id=censor_test_123" \
  -H "Content-Type: application/json" \
  -d '{"text": "Тестовая проверка цензуры"}'
```

##  Тестирование ошибок и граничных случаев

#### 9. Ошибки валидации
```bash
# Пустой комментарий
curl -X POST "http://localhost:8080/comments" \
  -H "Content-Type: application/json" \
  -d '{"news_id": 1, "text": ""}'

# Отсутствует news_id
curl -X POST "http://localhost:8080/comments" \
  -H "Content-Type: application/json" \
  -d '{"text": "Комментарий без новости"}'

# Неверный parent_id
curl -X POST "http://localhost:8080/comments" \
  -H "Content-Type: application/json" \
  -d '{"news_id": 1, "parent_id": 999999, "text": "Комментарий к несуществующему"}'

# Неверный формат JSON
curl -X POST "http://localhost:8080/comments" \
  -H "Content-Type: application/json" \
  -d '{"news_id": 1, "text": "Неверный JSON"'

# Несуществующая новость
curl "http://localhost:8080/news/999999"
curl "http://localhost:8080/comments/999999"
```

##  Мониторинг и здоровье системы

#### 10. Проверка состояния всех сервисов
```bash
# API Gateway не имеет /health, но можно проверить любой endpoint
curl "http://localhost:8080/news/latest?page=1"

# Comments Service
curl "http://localhost:8081/health"

# News Service  
curl "http://localhost:8082/health"

# Censorship Service
curl "http://localhost:8083/health"
```
