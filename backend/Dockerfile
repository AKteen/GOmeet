FROM golang:1.27-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY third_party ./third_party
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /api ./cmd/api

FROM alpine:3.20
RUN apk add --no-cache ca-certificates curl
COPY --from=build /api /api
EXPOSE 8000
# Render sets PORT. Local Compose leaves it unset and stays on 8000.
CMD ["sh", "-c", "/api migrate && /api serve"]
