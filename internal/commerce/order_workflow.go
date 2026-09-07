package commerce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

type ImageProcessor interface {
	Upload(context.Context, string, io.Reader, string) (string, error)
	Process(context.Context, string, int, int, string, string) (string, error)
}

type ThumbnailSpec struct {
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type ProductImage struct {
	Filename string
	Content  io.Reader
}

type Order struct {
	ID              string            `json:"id"`
	ProductSKU      string            `json:"product_sku"`
	State           string            `json:"state"`
	Checkout        string            `json:"checkout"`
	Fulfillment     string            `json:"fulfillment"`
	Receipt         string            `json:"receipt"`
	CustomerUpdate  string            `json:"customer_update"`
	ThumbnailURLs   map[string]string `json:"thumbnail_urls"`
}

type Workflow struct {
	Images ImageProcessor
	Specs  []ThumbnailSpec
}

func (w Workflow) PrepareCheckout(ctx context.Context, orderID, sku string, source ProductImage) (Order, error) {
	order := Order{ID: orderID, ProductSKU: sku, State: "media_processing", Checkout: "blocked", Fulfillment: "not_started", Receipt: "pending", CustomerUpdate: "pending", ThumbnailURLs: make(map[string]string)}
	uploaded, err := w.Images.Upload(ctx, source.Filename, source.Content, operationKey(orderID, "upload"))
	if err != nil {
		return order, err
	}
	for _, spec := range w.Specs {
		thumb, err := w.Images.Process(ctx, uploaded, spec.Width, spec.Height, "webp", operationKey(orderID, "thumbnail-"+spec.Name))
		if err != nil {
			return order, err
		}
		order.ThumbnailURLs[spec.Name] = thumb
	}
	order.State = "ready_for_checkout"
	order.Checkout = "open"
	order.CustomerUpdate = "product_media_ready"
	return order, nil
}

func CompleteOrder(order Order) Order {
	if order.State != "ready_for_checkout" {
		return order
	}
	order.State = "confirmed"
	order.Checkout = "paid"
	order.Fulfillment = "queued"
	order.Receipt = "issued"
	order.CustomerUpdate = "order_confirmed"
	return order
}

func operationKey(orderID, operation string) string {
	sum := sha256.Sum256([]byte(orderID + ":" + operation))
	return fmt.Sprintf("order-%s-%s", orderID, hex.EncodeToString(sum[:8]))
}
