.PHONY: build ui test serve

build: ui
	go build -o tsuji ./cmd/tsuji

ui:
	cd web && npm install && npm run build
	rm -rf pkg/webui/dist
	cp -R web/dist pkg/webui/dist

test:
	go vet ./...
	go test ./...

serve: build
	./tsuji serve
