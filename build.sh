rm client/bin/client
rm replica/bin/replica
go get github.com/go-redis/redis/v8@v8.11.5
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.30.0
protoc --go_out=. --go_opt=paths=source_relative proto/definitions.proto
go mod vendor
go build -v -o ./client/bin/client ./client/
go build -v -o ./replica/bin/replica ./replica/