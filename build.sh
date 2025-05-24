rm client/bin/client
rm replica/bin/replica
go get -u github.com/golang/protobuf/protoc-gen-go
go get -u google.golang.org/grpc
go get github.com/go-redis/redis/v8@v8.11.5
go mod vendor
#protoc --go_out=./ ./proto/definitions.proto
go build -v -o ./client/bin/client ./client/
go build -v -o ./replica/bin/replica ./replica/