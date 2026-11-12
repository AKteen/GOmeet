# GOmeet

One-to-one and one-to-many video calling. The API is Go. The UI is Next.js. Browsers send video and chat straight to Agora. The API stores accounts, rooms, and call logs, and mints the tokens.

```
Browser → Go API → PostgreSQL / Redis / Agora REST
       ↘ Agora RTC + Chat
```

## Local

```powershell
cd "C:\Users\vipad\GO meet"
copy backend\.env.example backend\.env
copy frontend\.env.example frontend\.env.local

docker compose up -d postgres redis

cd backend
go run ./cmd/api migrate
go run ./cmd/api serve
```

In another terminal:

```powershell
cd "C:\Users\vipad\GO meet\frontend"
npm install
npm run dev
```

- App: http://localhost:3000
- API: http://localhost:8000/health
- `NEXT_PUBLIC_API_URL=http://localhost:8000`

`docker compose up --build -d` runs Postgres, Redis, two API replicas, the web app, and Nginx at http://localhost. For that UI build, set `NEXT_PUBLIC_API_URL=http://localhost/api`.

## Render

Use [`render.yaml`](render.yaml). The API image runs migrations, then listens on `PORT`. A `postgres://` URL is rewritten for the Go driver, and TLS is turned on when `sslmode=require`.

GOmeet tables are created in the `gomeet` schema. The migration does not create or alter `public.users` or any Django table.

Fill these on the **gomeet-api** service. The blueprint already wires `DATABASE_URL` and `REDIS_URL`.

| Variable | Value |
|---|---|
| `JWT_SECRET` | Random string, at least 32 characters |
| `CORS_ORIGINS` | Vercel origin, `https://...`, no trailing slash |
| `AGORA_APP_ID` | Agora Console app ID |
| `AGORA_APP_CERTIFICATE` | Agora app certificate |
| `AGORA_CHAT_REST_HOST` | Agora Chat REST host |
| `AGORA_CHAT_APP_KEY` | Agora Chat app key |
| `AGORA_CUSTOMER_ID` | Optional. Needed for live captions |
| `AGORA_CUSTOMER_SECRET` | Optional. Needed for live captions |

`JWT_EXPIRE_MINUTES` is already `10080`. The service will not start on Render if `JWT_SECRET` is still the example, if `CORS_ORIGINS` is localhost or not https, or if the Agora app id, certificate, or Chat REST host is empty.

Health check: `/health`.

## Vercel

Create a Next.js project with the root directory set to `frontend`. Set these before the first build:

| Variable | Value |
|---|---|
| `NEXT_PUBLIC_API_URL` | `https://<gomeet-api>.onrender.com` |
| `NEXT_PUBLIC_AGORA_APP_ID` | Same Agora app ID as the API |
| `NEXT_PUBLIC_AGORA_CHAT_APP_KEY` | Agora Chat app key. Quote it if it contains `#` |

Do not put `AGORA_APP_CERTIFICATE` or `JWT_SECRET` on Vercel. Do not append `/api` to `NEXT_PUBLIC_API_URL`. That prefix is only for the local Nginx proxy.

After Vercel assigns a domain, set Render `CORS_ORIGINS` to that exact origin and redeploy the API if it was already live.

Camera and microphone need the https URLs both hosts provide.

## API

| Method | Path | Purpose |
|---|---|---|
| POST | `/auth/signup`, `/auth/login` | JWT |
| GET | `/auth/me` | Current user |
| POST | `/rooms` | Create room |
| GET | `/rooms/mine` | Rooms I host |
| GET | `/rooms/{slug}` | Room info |
| POST | `/rooms/{slug}/join` | RTC and chat tokens |
| POST | `/rooms/{slug}/heartbeat` | Presence |
| POST | `/rooms/{slug}/leave` | Leave and close the session |
| POST | `/rooms/{slug}/transcript` | Save a caption line |
| GET | `/rooms/{slug}/sessions` | Call logs |
| GET | `/health` | Health |

Errors use `{ "detail": "..." }`. Login is 10 requests per minute per IP. Join is 20 per minute per user. A full room returns 409.

```powershell
cd backend
go test ./...
```
