FROM golang:1.24

WORKDIR /app

COPY go.mod ./
RUN go mod download

# air をインストール
RUN go install github.com/air-verse/air@latest

COPY . .

EXPOSE 8080

CMD ["air"]
