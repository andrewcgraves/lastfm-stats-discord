FROM golang:1.19-alpine

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download

# COPY *.go ./
COPY . ./

RUN go build -o /discord-bot .

EXPOSE 8080

CMD [ "/discord-bot" ]