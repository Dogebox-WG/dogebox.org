default: dkm

.PHONY: clean, test
clean:
	rm -rf ./dkm

dkm: clean
	go build -o dkm .

dev:
	mkdir -p storage
	go run .

test:
	go test -v .
