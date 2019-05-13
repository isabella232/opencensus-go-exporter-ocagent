module contrib.go.opencensus.io/exporter/ocagent

require (
	github.com/census-instrumentation/opencensus-proto v0.2.2 // this is to match the version used in census-instrumentation/opencensus-service
	github.com/gogo/protobuf v1.2.1
	github.com/golang/protobuf v1.3.1
	github.com/grpc-ecosystem/grpc-gateway v1.8.5 // indirect
	go.opencensus.io v0.21.0
	google.golang.org/api v0.4.0
	google.golang.org/grpc v1.20.1
)

replace github.com/census-instrumentation/opencensus-proto => github.com/omnition/opencensus-proto v0.2.2-gogo2
