FROM golang:1.26

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Build once to fail fast on compile errors, then run the e2e suite.
RUN go build -o /usr/local/bin/wolfe it.kluth.buildcleaner
CMD ["go", "test", "-tags", "e2e", "-v", "./e2e/..."]
