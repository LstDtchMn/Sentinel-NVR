# Development-only image for the Sentinel NVR frontend.
# Uses Vite dev server with hot-reload; source is bind-mounted via docker-compose.
# NOTE: Runs as root intentionally — the bind mount (./frontend:/app) and
# anonymous node_modules volume require root for write access on Windows/macOS.
# A production Dockerfile would use a multi-stage build with USER node.
FROM node:22-slim

WORKDIR /app

COPY package.json package-lock.json* ./
# Use `npm ci` for reproducible installs from the lockfile.
# In dev, node_modules is overridden by docker-compose anonymous volume,
# but this layer ensures a fallback if the volume is empty.
# No `COPY . .` needed — source files are bind-mounted at runtime via docker-compose.
RUN npm ci

EXPOSE 5173

CMD ["npm", "run", "dev"]
