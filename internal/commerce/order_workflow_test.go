package commerce

import (
	"context"
	"io"
	"strings"
	"testing"
)

type fakeImages struct {
	failAt int
	calls  int
}

func (f *fakeImages) Upload(_ context.Context, _ string, _ io.Reader, _ string) (string, error) {
	f.calls++
	return "image-123", nil
}

func (f *fakeImages) Process(_ context.Context, _ string, _ int, _ int, _ string, _ string) (string, error) {
	f.calls++
	if f.calls == f.failAt {
		return "", io.ErrUnexpectedEOF
	}
	return "https://cdn.example/thumb.webp", nil
}

func TestPrepareCheckoutDecision(t *testing.T) {
	tests := []struct {
		name         string
		failAt       int
		wantState    string
		wantCheckout string
		wantErr      bool
	}{
		{name: "all thumbnails recorded", wantState: "ready_for_checkout", wantCheckout: "open"},
		{name: "thumbnail generation incomplete", failAt: 3, wantState: "media_processing", wantCheckout: "blocked", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			images := &fakeImages{failAt: tt.failAt}
			workflow := Workflow{Images: images, Specs: []ThumbnailSpec{{Name: "card", Width: 480, Height: 480}, {Name: "detail", Width: 960, Height: 1200}}}
			order, err := workflow.PrepareCheckout(context.Background(), "ord-7", "SKU-42", ProductImage{Filename: "shoe.jpg", Content: strings.NewReader("image")})
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if order.State != tt.wantState || order.Checkout != tt.wantCheckout {
				t.Fatalf("state/checkout = %s/%s, want %s/%s", order.State, order.Checkout, tt.wantState, tt.wantCheckout)
			}
		})
	}
}

func TestCompleteOrder(t *testing.T) {
	order := CompleteOrder(Order{State: "ready_for_checkout", Checkout: "open"})
	if order.Fulfillment != "queued" || order.Receipt != "issued" || order.CustomerUpdate != "order_confirmed" {
		t.Fatalf("unexpected completion: %+v", order)
	}
}
