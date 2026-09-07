package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"

	"example.com/product-thumbnail-orders/internal/commerce"
	"example.com/product-thumbnail-orders/internal/infrai"
)

type service struct {
	workflow commerce.Workflow
}

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	client := infrai.New(key)
	app := service{workflow: commerce.Workflow{Images: client, Specs: []commerce.ThumbnailSpec{{Name: "card", Width: 480, Height: 480}, {Name: "detail", Width: 960, Height: 1200}}}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders/prepare", app.prepare)
	mux.HandleFunc("POST /orders/complete", app.complete)
	log.Print("product order service listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func (s service) prepare(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(12 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart request"})
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "image is required"})
		return
	}
	defer file.Close()
	order, err := s.workflow.PrepareCheckout(r.Context(), r.FormValue("order_id"), r.FormValue("product_sku"), commerce.ProductImage{Filename: header.Filename, Content: file})
	if err != nil {
		var apiErr *infrai.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
			writeJSON(w, apiErr.HTTPStatus, map[string]string{"error": apiErr.Message})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "image processing did not complete"})
		return
	}
	writeJSON(w, http.StatusCreated, order)
}

func (s service) complete(w http.ResponseWriter, r *http.Request) {
	var order commerce.Order
	if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid order"})
		return
	}
	completed := commerce.CompleteOrder(order)
	if completed.State != "confirmed" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "order is not ready for checkout"})
		return
	}
	writeJSON(w, http.StatusOK, completed)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil && !strings.Contains(err.Error(), "closed") {
		log.Printf("encode response: %v", err)
	}
}
