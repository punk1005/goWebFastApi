package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/joho/godotenv/autoload"
)
var dbPool *pgxpool.Pool

func main() {
	// Сборка строки подключения из .env
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dbUser := os.Getenv("DB_USER")
	dbPass := os.Getenv("DB_PASS")
	dbName := os.Getenv("DB_NAME")

	if dbHost == "" || dbPort == "" || dbUser == "" || dbPass == "" || dbName == "" {
		log.Fatal("Ошибка: Не все настройки БД (DB_*) заполнены в .env")
	}

	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", 
		dbUser, dbPass, dbHost, dbPort, dbName,
	)

	var err error
	dbPool, err = pgxpool.New(context.Background(), connStr)
	if err != nil {
		log.Fatalf("Не удалось создать пул подключений: %v", err)
	}
	defer dbPool.Close()

	if err := dbPool.Ping(context.Background()); err != nil {
		log.Fatalf("БД недоступна: %v", err)
	}
	fmt.Println("Успешное подключение к PostgreSQL!")

	creator := os.Getenv("SERVER_CREATOR")
	if creator != "" {
		fmt.Printf("Создатель сервера: %s\n", creator)
	}

	// Настройка нового раздельного роутера
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", handleHome)
	mux.HandleFunc("GET /noparams/{endpoint}", handleNoParamsSQL)
	mux.HandleFunc("GET /get/{endpoint}", handleGetSQL)
	mux.HandleFunc("POST /post/{endpoint}", handlePostSQLStub) // Заглушка для POST

	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Printf("Сервер запущен на http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

// Стартовая страница
func handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("<h1>Привет! Добро пожаловать на универсальный SQL-сервер.</h1>"))
}

// Хэндлер 1: Запросы БЕЗ параметров (/noparams/...)
func handleNoParamsSQL(w http.ResponseWriter, r *http.Request) {
	endpoint := getSafeEndpoint(r.PathValue("endpoint"))
	sqlPath := filepath.Join("sql/noparams", endpoint+".sql")

	if !fileExists(sqlPath) {
		http.Error(w, "Эндпоинт не найден", http.StatusNotFound)
		return
	}

	query, err := os.ReadFile(sqlPath)
	if err != nil {
		http.Error(w, "Ошибка чтения SQL", http.StatusInternalServerError)
		return
	}

	// Выполняем без аргументов
	executeAndSend(w, string(query))
}

// Хэндлер 2: GET-запросы С параметрами (/get/...?)
func handleGetSQL(w http.ResponseWriter, r *http.Request) {
	endpoint := getSafeEndpoint(r.PathValue("endpoint"))
	sqlPath := filepath.Join("sql/get", endpoint+".sql")

	if !fileExists(sqlPath) {
		http.Error(w, "Эндпоинт не найден", http.StatusNotFound)
		return
	}

	query, err := os.ReadFile(sqlPath)
	if err != nil {
		http.Error(w, "Ошибка чтения SQL", http.StatusInternalServerError)
		return
	}

	// Собираем ВСЕ значения параметров из URL по порядку их перечисления
	queryParams := r.URL.Query()
	var args []interface{}
	
	for _, values := range queryParams {
		if len(values) > 0 {
			args = append(args, values[0]) // Берем первое значение параметра
		}
	}

	// Выполняем запрос, передавая собранные аргументы
	executeAndSend(w, string(query), args...)
}

// Хэндлер 3: POST-запросы С параметрами из JSON-тела (/post/...)
func handlePostSQLStub(w http.ResponseWriter, r *http.Request) {
	endpoint := getSafeEndpoint(r.PathValue("endpoint"))
	sqlPath := filepath.Join("sql/post", endpoint+".sql")

	if !fileExists(sqlPath) {
		http.Error(w, "Эндпоинт не найден", http.StatusNotFound)
		return
	}

	query, err := os.ReadFile(sqlPath)
	if err != nil {
		http.Error(w, "Ошибка чтения SQL", http.StatusInternalServerError)
		return
	}

	// 1. Декодируем входящий JSON в динамическую map
	var bodyMap map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&bodyMap); err != nil {
		// Если тело пустое, создаем пустую мапу
		bodyMap = make(map[string]interface{})
	}
	defer r.Body.Close()

	// 2. Сортируем ключи по алфавиту для строгой последовательности аргументов
	var keys []string
	for k := range bodyMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 3. Собираем аргументы по отсортированным ключам
	var args []interface{}
	for _, k := range keys {
		args = append(args, bodyMap[k])
	}

	// 4. Выполняем SQL-запрос (это может быть INSERT, UPDATE или SELECT)
	executeAndSend(w, string(query), args...)
}

// --- ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ (ЧТОБЫ НЕ ДУБЛИРОВАТЬ КОД) ---

// Безопасное извлечение имени файла
func getSafeEndpoint(param string) string {
	param = filepath.Base(param)
	return strings.TrimSuffix(param, ".sql")
}

// Проверка существования файла
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// Общая функция выполнения SQL и отправки JSON клиенту
func executeAndSend(w http.ResponseWriter, query string, args ...interface{}) {
	ctx := context.Background()
	rows, err := dbPool.Query(ctx, query, args...)
	if err != nil {
		http.Error(w, fmt.Sprintf("Ошибка БД: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	fieldDescriptions := rows.FieldDescriptions()
	var result []map[string]interface{}

	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			http.Error(w, "Ошибка парсинга данных", http.StatusInternalServerError)
			return
		}

		rowMap := make(map[string]interface{})
		for i, fd := range fieldDescriptions {
			rowMap[fd.Name] = values[i]
		}
		result = append(result, rowMap)
	}

	if result == nil {
		result = []map[string]interface{}{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}