# EzMonitor frontend: build the Vite app, serve the static files with Caddy.
# The root Caddyfile proxies "/" here and "/api/*" to the backend, so the app
# calls the API on the same origin (VITE_API_URL left empty).
FROM node:24-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY . .
ENV VITE_API_URL="" VITE_MOCK_QUERY_API=false
RUN npm run build

FROM caddy:2-alpine
COPY --from=build /app/dist /srv
# SPA: unknown paths (e.g. /groups/3) fall back to index.html for React Router.
RUN printf ':80 {\n\troot * /srv\n\tencode zstd gzip\n\ttry_files {path} /index.html\n\tfile_server\n}\n' > /etc/caddy/Caddyfile
EXPOSE 80
