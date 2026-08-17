package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port           string
	AdminToken     string
	DataDir        string
	MaxBreadcrumbs int
	WorkerCount    int
	BufferSize     int
	RateLimitRPS   float64
	GeminiAPIKey   string
}

func LoadConfig() *Config {
	return &Config{
		Port:           getEnv("PORT", "8080"),
		AdminToken:     getEnv("SIGTRAP_ADMIN_TOKEN", "dev_admin_secret_123"),
		DataDir:        getEnv("SIGTRAP_DATA_DIR", "./data"),
		MaxBreadcrumbs: getEnvAsInt("SIGTRAP_MAX_BREADCRUMBS", 50),
		WorkerCount:    getEnvAsInt("SIGTRAP_WORKER_COUNT", 4),
		BufferSize:     getEnvAsInt("SIGTRAP_BUFFER_SIZE", 1000),
		RateLimitRPS:   getEnvAsFloat("SIGTRAP_RATE_LIMIT_RPS", 100.0),
		GeminiAPIKey:   getEnv("GEMINI_API_KEY", ""),
	}
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}

func getEnvAsInt(key string, fallback int) int {
	if valStr, ok := os.LookupEnv(key); ok && valStr != "" {
		if val, err := strconv.Atoi(valStr); err == nil {
			return val
		}
	}
	return fallback
}

func getEnvAsFloat(key string, fallback float64) float64 {
	if valStr, ok := os.LookupEnv(key); ok && valStr != "" {
		if val, err := strconv.ParseFloat(valStr, 64); err == nil {
			return val
		}
	}
	return fallback
}
