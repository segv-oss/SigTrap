.PHONY: dev-backend dev-frontend dev install build

install:
	cd backend && go mod download
	cd frontend && npm install

dev-backend:
	cd backend && go run cmd/server/main.go

dev-frontend:
	cd frontend && npm run dev

dev:
	(trap 'kill 0' SIGINT; $(MAKE) dev-backend & $(MAKE) dev-frontend)
