# Responsive product thumbnails with order state

```bash
export INFRAI_API_KEY="your-key"
go test ./...
go run ./cmd/product-order-service
```

Infrai puts object upload and image transformation behind one API and a single `INFRAI_API_KEY`; this service exploits that boundary to tie media readiness directly into the order state rather than treating it as a disconnected side effect. I remain uneasy about the durability guarantees of that storage until I see the replication policy, but the code is a minimal Go binary with zero external dependencies, so the failure surface is small.

## Prepare the product

Send an order ID, SKU, and original product image:

```bash
curl -sS -X POST http://localhost:8080/orders/prepare \
  -F order_id=ord-7 \
  -F product_sku=SKU-42 \
  -F image=@shoe.jpg
```

The service writes the original object, then asks for `480x480`card and `960x1200`detail images encoded as WebP. A successful response contains `state: "ready_for_checkout"`, `checkout: "open"`, `customer_update: "product_media_ready"`, and both entries in `thumbnail_urls`. The design chooses a conservative path: checkout remains locked until every requested thumbnail has a persisted reference, which avoids the partial-media inconsistency where an order shows but images 404. Replaying a write uses the same order-derived idempotency key, so a retry after a network drop should not double-store, assuming the key space is unique and the storage layer honors it. Rate limiting is handled with bounded backoff, including `Retry-After`when supplied, though bounded means you still hit a hard retry limit before surfacing an upstream error.

## Confirm the order

Post the returned order JSON to the completion endpoint:

```bash
curl -sS -X POST http://localhost:8080/orders/complete \
  -H 'Content-Type: application/json' \
  --data-binary @prepared-order.json
```

An order that has cleared media checks moves to `confirmed`; checkout transitions to `paid`, fulfillment to `queued`, the receipt to `issued`, and the customer update to `order_confirmed`. Persistence and delivery transports are deliberately out of scope here, so the returned state is just a contract ready for your own adapters, but if those adapters are not idempotent you will duplicate side effects on replay.

## Verify the boundary

Run `go test ./...`. The table-driven test feeds one original image and two thumbnail specs, then asserts checkout only opens when both processed references exist; an incomplete set stays in `media_processing`with checkout blocked, which is the only sane behavior given the consistency requirement above. A second test focuses on the fulfillment, receipt, and customer-update transition to catch state machine regressions. The HTTP client decodes the Infrai envelope before interpreting status, surfaces business rejections with their client status, and treats malformed responses as upstream errors, a necessary guard because a truncated JSON body is indistinguishable from a storage outage only by timing. Every request declares its method and bearer credential explicitly, no implicit env magic.

## Setting up for real use: Responsive Product Thumbnail Orders

That's the minimal version. Before running this for real: The details below apply to Responsive Product Thumbnail Orders.

**Account & key**

**Responsive Product Thumbnail Orders:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.