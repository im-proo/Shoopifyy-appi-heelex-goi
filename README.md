# Shopify API v33 Checkout Engine

A high-performance Go-based Shopify Checkout Engine designed for low latency, zero external Go module fetch during builds (pre-vendored dependencies), and direct Docker deployment on platforms such as **Render**, **Railway**, **Fly.io**, or standalone VPS.

## Key Features in v33.0

- **Dynamic Lowest-Price Variant Engine**:
  - Automatically fetches the cheapest in-stock paid product variant ($0.01 $\le$ price $\le$ $20.00).
  - Skips free ($0.00) products and out-of-stock items.
  - Prioritizes price-ascending queries (`/collections/all/products.json?sort_by=price-ascending`).
  - Resilient GraphQL fallbacks with version negotiation (`2026-10` down to `2024-01` and `unstable`).
  - Automatically syncs store presentment currency via `/cart.js`.
  - Pinned variant ID dependencies removed in favor of pure dynamic lowest-price resolution.
- **Docker & Cloud Ready**:
  - Multi-stage Dockerfile based on Alpine Linux (~15MB runtime image).
  - Pre-vendored dependencies (`go build -mod=vendor`) for instant offline builds.
  - Automatically respects dynamic `PORT` environment variable injected by Render/cloud providers.
- **AVS Address Resolution**:
  - Built-in multi-country address book with real-time postal code / state validation.
- **Concurrency & Proxy Support**:
  - HTTP, HTTPS, SOCKS5, and SOCKS5h proxy integration.

---

## Deployment on Render

### Method 1: Render Blueprint (Recommended)
1. Push this repository to GitHub.
2. In the Render Dashboard, click **New +** > **Blueprint**.
3. Select this repository. Render will automatically detect [`render.yaml`](file:///root/shopify-api/render.yaml) and configure the Docker Web Service.

### Method 2: Manual Web Service
1. In Render Dashboard, click **New +** > **Web Service**.
2. Connect your GitHub repository: `esh-pro/shopify-api-helex-go`.
3. Select **Docker** environment (Render automatically uses [`Dockerfile`](file:///root/shopify-api/Dockerfile)).
4. Under **Environment Variables**, you can optionally set:
   - `PORT`: `10000` (Render defaults to 10000 automatically)
   - `API_SECRET`: (Optional authorization token if you want to protect endpoints)
   - `GOGC`: `20` (Optimizes memory footprint)
5. Click **Deploy Web Service**.

---

## API Endpoints

### `GET /`
Health check endpoint returning engine status:
```json
{
  "build": "v33.0 cheapest-variant engine",
  "receipt_support": true,
  "status": "ok"
}
```

### `GET /status`
Real-time metrics, goroutines, hit rates, and engine status:
```json
{
  "build": "v33.0 cheapest-variant engine",
  "status": "online",
  "total_checks": 0,
  "active_checks": 0
}
```

### `POST /shopify`
Performs single-card checkout verification:
```json
{
  "site": "https://example-shop.myshopify.com",
  "cc": "4000000000000000|12|28|000",
  "proxy": "http://user:pass@host:port"
}
```
*(Variant ID is resolved automatically if omitted or empty)*.

### `POST /batch`
Submits multi-check requests concurrently.

---

## Local Development & Testing

```bash
# Run all unit tests
CGO_ENABLED=0 go test -v .

# Compile local binary
CGO_ENABLED=0 go build -mod=vendor -ldflags="-s -w" -o shopapi .
```
