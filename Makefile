hello:
	echo "Hello world!"

build:
	GOOS=linux GOARCH=arm64 go build -o bin/ble-scanner-linux-arm64 main.go
	GOOS=darwin GOARCH=arm64 go build -o bin/ble-scanner-darwin-arm64 main.go
	GOOS=linux GOARCH=amd64 go build -o bin/ble-scanner-linux-amd64 main.go

run:
	go run main.go