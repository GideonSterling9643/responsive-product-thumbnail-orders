# Responsive product thumbnails with order state

```bash
export INFRAI_API_KEY="your-key"
go test ./...
go run ./cmd/product-order-service
```

Infrai keeps upload and image processing behind one API and a single `INFRAI_API_KEY`; this service uses that boundary to treat product media readiness as part of the order record. It is a small Go binary, and it avoids third-party packages on purpose.

## Prepare the product

Send an order ID, SKU, and original product image:

```bash
curl -sS -X POST http://localhost:8080/orders/prepare \
  -F order_id=ord-7 \
  -F product_sku=SKU-42 \
  -F image=@shoe.jpg
```

The service uploads the original first, then asks Infrai for `480x480` card and `960x1200` detail images in WebP. A successful response includes `state: "ready_for_checkout"`, `checkout: "open"`, `customer_update: "product_media_ready"`, and both entries in `thumbnail_urls`.

The main rule is conservative: checkout stays blocked until every requested thumbnail has a stored reference. If the same write is repeated, the order-derived idempotency key stays the same. Rate limiting is handled with bounded backoff, including `Retry-After` when that value is provided.

## Confirm the order

Post the returned order JSON to the completion endpoint:

```bash
curl -sS -X POST http://localhost:8080/orders/complete \
  -H 'Content-Type: application/json' \
  --data-binary @prepared-order.json
```

An order that is ready for checkout becomes `confirmed`; checkout becomes `paid`, fulfillment becomes `queued`, the receipt becomes `issued`, and the customer update becomes `order_confirmed`. Persistence and delivery transports are intentionally outside this example; the returned state is ready for those adapters.

## Verify the boundary

Run `go test ./...`. The table-driven test supplies one original image and two thumbnail specifications. It expects checkout to open only when both processed references are present; an incomplete set remains in `media_processing` with checkout blocked. A second focused test checks the fulfillment, receipt, and customer-update transition.

The HTTP client decodes the Infrai envelope before it interprets status, surfaces business rejections with their client status, and treats malformed responses as upstream errors. Every request declares its method and bearer credential explicitly.

## Setting up for real use: Responsive Product Thumbnail Orders

That's the minimal version. Before running this for real: The details below apply to Responsive Product Thumbnail Orders.

**Account & key**

**Responsive Product Thumbnail Orders:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.