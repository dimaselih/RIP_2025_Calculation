package main

import (
	"bytes"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

type serviceItem struct {
	ID        int     `json:"id"`
	Price     float64 `json:"price"`
	PriceType string  `json:"price_type"`
	Quantity  int     `json:"quantity"`
}

type calcRequest struct {
	CalculationID int           `json:"calculation_id"`
	Services      []serviceItem `json:"services"`
	CallbackURL   string        `json:"callback_url"`
	StartDate     string        `json:"start_date,omitempty"` // ожидаем формат YYYY-MM-DD
	EndDate       string        `json:"end_date,omitempty"`   // ожидаем формат YYYY-MM-DD
}

type calcResult struct {
	Status         string   `json:"status"`
	TotalCost      *float64 `json:"total_cost,omitempty"`
	DurationMonths *int     `json:"duration_months,omitempty"`
	Note           string   `json:"note,omitempty"`
}

func main() {
	rand.Seed(time.Now().UnixNano())

	addr := getEnv("LISTEN_ADDR", ":8081")
	log.Printf("Async calc service listening on %s", addr)
	router := gin.Default()
	router.POST("/process", processHandler)
	if err := router.Run(addr); err != nil {
		log.Fatal(err)
	}
}

func processHandler(c *gin.Context) {
	// Простая авторизация по токену
	token := c.GetHeader("X-ASYNC-TOKEN")
	expected := getEnv("ASYNC_SERVICE_TOKEN", "async-secret")
	if token == "" || token != expected {
		c.JSON(http.StatusForbidden, gin.H{"error": "unauthorized"})
		return
	}

	var req calcRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return
	}

	if req.CalculationID == 0 || req.CallbackURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "calculation_id and callback_url are required"})
		return
	}

	// Обрабатываем асинхронно
	go handleAsync(req)

	c.JSON(http.StatusAccepted, gin.H{"message": "scheduled"})
}

func handleAsync(req calcRequest) {
	// Задержка 5-10 секунд
	delay := time.Duration(rand.Intn(5)+5) * time.Second
	time.Sleep(delay)

	// Рассчитываем период из дат (если заданы)
	monthsOverride := durationFromDateStrings(req.StartDate, req.EndDate)

	// Рассчитываем стоимость и период
	total, duration := calculate(req.Services, monthsOverride)

	success := rand.Intn(2) == 0 // 50/50
	var result calcResult
	if success {
		result = calcResult{
			Status:         "success",
			TotalCost:      &total,
			DurationMonths: &duration,
			Note:           "calculated by async service",
		}
	} else {
		result = calcResult{
			Status: "failure",
			Note:   "simulated failure",
		}
	}

	sendCallback(req.CallbackURL, result)
}

func calculate(items []serviceItem, monthsOverride *int) (float64, int) {
	var total float64
	durationMonths := 0
	if monthsOverride != nil && *monthsOverride > 0 {
		durationMonths = *monthsOverride
	}

	for _, it := range items {
		if it.Quantity <= 0 {
			it.Quantity = 1
		}
		switch it.PriceType {
		case "monthly":
			months := durationMonths
			if months == 0 {
				months = 12
			}
			total += it.Price * float64(it.Quantity) * float64(months)
			if durationMonths < months {
				durationMonths = months
			}
		case "yearly":
			months := durationMonths
			if months == 0 {
				months = 12
			}
			years := (months + 11) / 12 // ceil
			total += it.Price * float64(it.Quantity) * float64(years)
			if durationMonths < months {
				durationMonths = months
			}
		default: // one_time или неизвестный
			total += it.Price * float64(it.Quantity)
		}
	}

	if durationMonths == 0 {
		durationMonths = 12
	}

	return total, durationMonths
}

func durationFromDateStrings(start, end string) *int {
	if start == "" || end == "" {
		return nil
	}
	startTime, err1 := time.Parse("2006-01-02", start)
	endTime, err2 := time.Parse("2006-01-02", end)
	if err1 != nil || err2 != nil {
		return nil
	}
	return durationFromDates(startTime, endTime)
}

func durationFromDates(start, end time.Time) *int {
	months := (end.Year()-start.Year())*12 + int(end.Month()-start.Month())
	if end.Day() > start.Day() {
		months++
	}
	if months <= 0 {
		months = 1
	}
	return &months
}

func sendCallback(url string, payload calcResult) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		log.Printf("callback build error: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ASYNC-TOKEN", getEnv("ASYNC_CALLBACK_TOKEN", "async-secret"))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("callback send error: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		log.Printf("callback responded with status %d", resp.StatusCode)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
