# Zentech Tasks

Task app for `task.zentechglobal.io`.

## Stack

- Backend: Go, OAuth client for `https://id.zentechglobal.io`
- Frontend: Vue 3 + Vite
- Login: redirects to `id.zentechglobal.io` with `client_id=task`

## Local Development

Backend:

```bash
cd backend
TASK_COOKIE_SECURE=false \
TASK_ID_ISSUER=http://localhost:8080 \
TASK_ID_REDIRECT_URI=http://localhost:5173/auth/callback \
go run .
```

Frontend:

```bash
cd frontend
npm install
npm run dev
```

For local OAuth, add this redirect URI to the identity server site:

```text
http://localhost:5173/auth/callback
```

## Production

Identity site/client:

```text
slug: task
name: Zentech Tasks
redirect_uris: https://task.zentechglobal.io/auth/callback
```

Run:

```bash
docker compose up -d --build
```

Reverse proxy `task.zentechglobal.io` to `127.0.0.1:18082`.
