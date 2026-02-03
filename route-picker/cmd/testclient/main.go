package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	stdlog "log"
	"time"

	core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	service_ext_proc_v3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
)

func main() {
	addr := flag.String("addr", "localhost:18080", "gRPC server address")
	fullFlow := flag.Bool("full-flow", false, "Run full flow test with body")
	flag.Parse()

	if *fullFlow {
		testFullFlow()
		return
	}

	conn, err := grpc.Dial(*addr, grpc.WithInsecure())
	if err != nil {
		stdlog.Fatalf("failed to dial %s: %v", *addr, err)
	}
	defer conn.Close()

	client := service_ext_proc_v3.NewExternalProcessorClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := client.Process(ctx)
	if err != nil {
		stdlog.Fatalf("failed to create Process stream: %v", err)
	}

	// Build a ProcessingRequest with empty RequestHeaders. The server will
	// add the `x-routing-decision` header by default so we don't send
	// an `instructions` header from the client anymore.
	req := &service_ext_proc_v3.ProcessingRequest{
		Request: &service_ext_proc_v3.ProcessingRequest_RequestHeaders{
			RequestHeaders: &service_ext_proc_v3.HttpHeaders{
				Headers: &core_v3.HeaderMap{
					Headers: []*core_v3.HeaderValue{},
				},
			},
		},
	}

	// Send the request
	if err := stream.Send(req); err != nil {
		stdlog.Fatalf("failed to send ProcessingRequest: %v", err)
	}

	// Close the send direction to indicate no more client messages
	if err := stream.CloseSend(); err != nil {
		stdlog.Printf("CloseSend error: %v", err)
	}

	// Receive responses
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			stdlog.Fatalf("stream.Recv error: %v", err)
		}
		if jb, err := protojson.Marshal(resp); err != nil {
			stdlog.Printf("response marshal error: %v", err)
		} else {
			fmt.Printf("Received ProcessingResponse: %s\n", string(jb))
		}
	}

	fmt.Println("done")
}
